// Package sandboxfs gives a task its own filesystem environment: a
// read-through union of the host filesystem and a sqlite-backed upper layer
// that records every mutation, serves the session's reads, lists the net
// changes, and applies selected changes back to the host.
// See TheoryOfSandboxFilesystem.
package sandboxfs

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

const TheoryOfSandboxFilesystem = `
A task runs inside one filesystem environment built from two layers. The
base layer is the host filesystem, read through unchanged, so the model
reads real data without copying it into the sandbox. The upper layer is a
sqlite database file that records every mutation: content, kind, mode, and
the base state of the path as it stood when the session first touched it.
One session's filesystem state is therefore one portable, browsable file.

The boundary is the filesystem, not the tool's file abstraction. Change
blocks keep applying atomically through changes.FileStore, and FileStore
never learns that the sandbox exists: the sandbox intercepts below it, at
the mount namespace, so it covers every reader and writer — the toolchain,
shell commands, and any other subprocess — and not only the change-block
path.

Reads fall through to the base except where the upper layer overrides the
path; a whiteout hides a deleted base file, and a directory listing merges
both layers. Every mutation writes an audit row, and a path's first read
writes one too: the audit answers which files a session touched, not how
often it read them.

A path's net change is derived from hashes, never from a stored second copy
of the content: the first mutation stores the base content hash, so a path
rewritten to its original bytes — or created and deleted in one session —
shows no change. Applying a change writes the upper content to the host and
drops the upper row, so the next diff is exact and a repeated apply is a
no-op.

The store never targets its own database. The review session holds the
database open for its whole life, and its records are the review surface; a
sandbox mutation of the database file — or of SQLite's -wal and -shm
siblings — would corrupt the record the review depends on. The store
refuses such a mutation before it reads the base and omits the path's net
change, so the path never enters the reviewable set. See
TheoryOfCompletePOSIX for how the file's SQLite siblings arise.

The environment is deliberately not a fresh isolated copy. The model must
reach the host's real data; the safety comes from intercepting writes and
making them reviewable, not from hiding data. This is also why a plain
overlay filesystem does not serve: it offers no read or write hook to
audit, intercept, or defer a write, while this upper layer offers all
three.
`

const TheoryOfReadPath = `
The read path is the hot path of a coding task: tooling stats, lists, and
reads thousands of paths while a session writes a few dozen. Every
optimization here leaves the write path untouched.

A metadata lookup never loads content. Stat needs the kind, mode, mtime,
and size; the row's metadata is the whole answer, and the content lives in the
chunk table. The size therefore comes from the row's stored size column, so a
getattr or lookup on a large file costs one small row, not a heap copy of the
file. A read's window, kind, and deletion state come back in one indexed query
over the chunks covering the window, so one kernel-sized read is one round trip
instead of a lookup plus a window.

A read handle opens the base file once. The kernel streams a file in
windows; reopening and re-checking the host file per window spends a stat
and an open on data the mount already made readable. The handle keeps the
descriptor it opened, and the host type is checked once, before the open,
so a pipe is never opened and its read never blocks.

The cache never shadows the session. Every read asks the upper layer
first, so a written path serves its new content and a whiteout reports the
deletion without any invalidation. A truncation — the truncating open or
setattr's size change — drops every reader cached for the path, and a
write drops its own handle's reader and every other handle's, because the
content that was just written is the store content from then on. The
cached descriptor fixes a base file's view at open, the same snapshot the
review step compares the host against when it reports a conflict.
`

const TheoryOfUpperAbsentCache = `
The read path is the hot path, and most of its paths never enter the upper
layer: a dependency the session only reads is answered by the base. An
uncached negative lookup pays a sqlite round trip for every one of them, and
that round trip dominates a read request which otherwise costs one base read.

The cache therefore records the paths a lookup found absent, and clears a
path's entry whenever the store writes the path. put is the only writer of
the table — a row appears or changes only through it — so a cleared entry is
refreshed on the next lookup, and a cached absence states exactly what the
table said when it was recorded. The apply step only removes rows, which can
make an absence more true, never less.

The cache is bounded by the paths a session touches, and the audit trail
already spends one record per such path, so it adds no memory the session did
not already spend. One mutex guards it, the same lock the read audit takes.
`

const TheoryOfWriteSplice = `
The write path grows with the payload, never with the file. A kernel WRITE
carries at most one negotiated request window of bytes. The store writes that
window's bytes into the chunks the payload covers, so a window costs its own
bytes and the two chunks it begins and ends inside; it inserts no zeros for a
gap, because an absent chunk reads as zeros, so a write past the stored end
costs its size update and its payload. A ten-megabyte file written in
one-megabyte windows therefore moves ten megabytes, never the growing prefix
an in-place whole-blob rewrite would copy once per window.

The store is the only copy. A handle holds no buffer, so a large open file
costs no memory and a write needs no reload before its delta: the first write
of a fresh path materialises the row — capturing the base state and the base
content the delta starts from — and every write, first or later, lands its own
payload. Two handles over one path therefore cannot diverge: there is no stale
buffer to overwrite another handle's bytes, and the kernel's next read asks the
store for the content the last write left there.

The splice runs in one transaction whose first statement both guards the row
and extends its size, so the delta's admissibility and its length cost one
statement, and one more statement carries every covered chunk. A path the
upper layer holds no live regular row for refuses the splice, the caller
materialises it, and the splice runs once more; the hot path therefore costs
one commit and never a second lookup of the content this write exists to avoid
copying.
`

const TheoryOfApplyConflict = `
Write-write conflicts are detected at apply time, against the snapshot the
session computed its changes from. The stored base hash is that snapshot: a
host file whose current content no longer hashes to it changed after the
session read it, and applying would discard the outside change. The same
holds in the other direction: a path the session created, but which now
exists on the host, and a path the session deleted, but which the host no
longer holds. Such a change is reported with its conflict reason and never
applied silently; the user resolves it.

Detection reads the host, so it belongs to the review step, not to every
read: a session pays nothing for it until it asks what changed or applies.
The hash is content-addressed, so it catches a same-size rewrite that an
mtime comparison would miss.
`

const TheoryOfCompletePOSIX = `
Complete POSIX coverage means the operations real tooling performs, not
every opcode the protocol defines. The Go toolchain, shells, and editors
read and write files, walk directories, rename, link, create named pipes,
and set times. Extended attributes, IOCTL, POSIX byte-range locks, and hole
queries reach the store as ENOSYS; the kernel then degrades as it does for
any filesystem without them, and the tooling that needs them — capability
xattrs, device management — is outside the coding tasks this environment
serves.

A named pipe is recorded by existence and mode alone: the store never opens
it, because pipe reads and writes are kernel objects whose blocking
semantics a filesystem does not route through FUSE. Applying a pipe
recreates the node with MKNODAT. A read or write addressed to a pipe path
reports EINVAL: the store cannot emulate blocking, and returning content
would be worse than an honest refusal.

Times are recorded, not served: SETATTR's mtime bits replace the stored
mtime, because a toolchain reads mtime to judge staleness, and a caller
that sets the time expects the next read to report it. atime is not part of
the recorded identity and is dropped.

Open flags are honoured where the kernel routes them to the filesystem.
CREATE carries O_CREAT, O_EXCL, and O_TRUNC: an existing path is refused
under O_EXCL, kept under O_CREAT alone, and emptied under O_TRUNC. A plain
OPEN with O_TRUNC truncates too — POSIX gives the flag the same effect
whether or not the file is created — and a write-mode OPEN is checked
against the write policy first, so a caller sees EACCES from open rather
than from its first write.

The read path streams: a read serves the requested window from the chunks
covering it, or from the base file's own ReadAt, so a large dependency costs
one sized response rather than a whole-file allocation. The write path
splices: a window rewrites the chunks its payload covers, so it costs bytes
proportional to its payload and no handle holds a copy of the file (see
TheoryOfWriteSplice). A zero-byte write transfers nothing and does not extend
the file, matching Linux. Truncation drops the chunks beyond the new length
and keeps the boundary chunk's prefix, and a grow raises the length alone
because an absent chunk reads as zeros; a grow past the size bound is refused
with EFBIG rather than allocating a file the sandbox cannot hold, and the same
bound refuses a write whose offset would overflow. See TheoryOfFuseMount.

Durability: FSYNC and FSYNCDIR succeed without doing anything, because the
store's writes are sqlite transactions that persist as they complete. A
tool that fsyncs to force data to disk gets the answer it expects, and the
data has already reached the sandbox database by the time the call returns.

RENAME2's flags are honoured where the store can honour them: NOREPLACE
refuses an existing destination with EEXIST. EXCHANGE and WHITEOUT need an
atomic inode swap that a path-addressed store cannot provide, so they are
refused with ENOSYS and a caller that requires them falls back to the plain
form. The plain rename is the one path the toolchain uses, and its
semantics are what the store implements: a file or link replaces the
destination, a directory replaces an existing empty destination directory,
a populated destination reports ENOTEMPTY, a non-directory destination
reports ENOTDIR, and a destination inside the source reports EINVAL.

One deviation is known and accepted for the initial version: a file opened
for reading and then unlinked stops serving reads, because the store keeps
no detached-inode reference. A toolchain that writes a temp file, renames
it, and reads the renamed path never hits the case; an fd that survives its
own unlink does. Lazy loading is the trade-off: the open path never copies
the file into memory on the hope a write follows.

Hard links copy content instead of sharing an inode: the store is
path-addressed and has no inode to link. The two paths hold the same bytes
after the link and evolve independently afterwards. The Go build cache,
which does rely on link sharing, lives in a passthrough directory and
therefore never reaches the store.
`

const TheoryOfWritePolicy = `
A write policy confines the store to the directories the surrounding
environment already makes writable. The store asks the policy about every
path it is about to mutate, before it changes anything, so a refused write
leaves no trace; the resulting error carries os.ErrPermission, which the
FUSE layer maps to EACCES.

The policy is a function of the path alone. It must not read the filesystem
it serves: the FUSE server answers every request on one goroutine, so a
policy that opened a path through the mount would block on the request its
own call is answering. The writable set is therefore resolved once before
the mount and matched by path prefix, not by resolving symlinks per write.

Write confinement is a second barrier, not the review mechanism. A write
the policy admits still lands in the upper layer and still needs the user's
apply, so the policy's job is to keep the store from recording writes the
environment never allowed — the same set the mount layer binds read-write —
and the review step's job is to let the user choose.
`

// TheoryOfAuditBatching explains how the audit trail trades a little
// durability for one commit per batch: audit buffers a row and writes the
// buffer when it fills, and Ops and Close flush the tail.
const TheoryOfAuditBatching = `
The audit trail records what a session did to which path. Its rows are
buffered and written in batches: audit appends a row and writes the buffer in
one transaction when it reaches auditBatchSize, and Ops and Close flush the
tail, so one commit serves a whole batch.

A commit dominates a small statement's cost in this store, so a row written
per commit charged every mutation and every path's first read a commit of its
own beside the operation's own statement. Batching removes that second commit
from the request path without changing what the trail records.

The trade-off is the tail: rows buffered when a process dies outside a close
are lost, so the trail is complete for a session that ends normally and may be
short for one that was killed. The audit is best-effort by design — it never
fails an operation — and the review step reads changes from the content rows,
which stay durable at every write. Rows keep their call order within a batch;
under concurrent mutation a later batch can commit before an earlier one, so
Ops orders by insertion id and each batch keeps its own order.
`

const schema = `
CREATE TABLE IF NOT EXISTS files (
    path TEXT PRIMARY KEY,
    kind INTEGER NOT NULL,
    mode INTEGER NOT NULL,
    mtime INTEGER NOT NULL,
    deleted INTEGER NOT NULL DEFAULT 0,
    size INTEGER NOT NULL DEFAULT 0,
    target TEXT NOT NULL DEFAULT '',
    original_exists INTEGER NOT NULL DEFAULT 0,
    original_hash TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS chunks (
    path TEXT NOT NULL,
    idx INTEGER NOT NULL,
    data BLOB NOT NULL,
    PRIMARY KEY (path, idx)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS ops (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    time TEXT NOT NULL,
    op TEXT NOT NULL,
    path TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ops_path ON ops(path);
`

// Kind is the file type of a sandbox path.
type Kind int

const (
	// KindRegular is a regular file.
	KindRegular Kind = iota
	// KindDir is a directory.
	KindDir
	// KindSymlink is a symbolic link; its target is stored instead of
	// content, and the kernel resolves it by reading the link and looking
	// up the target path, so the store never follows a link itself.
	KindSymlink
)

// KindFIFO is a named pipe. The store records its existence and mode only:
// a pipe carries no content and its blocking semantics belong to the
// kernel, which routes pipe reads and writes without asking the filesystem.
// See TheoryOfCompletePOSIX.
const KindFIFO Kind = 3

// Attr describes one sandbox path.
type Attr struct {
	Kind    Kind
	Mode    os.FileMode
	Size    int64
	ModTime time.Time
}

// DirEntry is one entry of a sandbox directory listing.
type DirEntry struct {
	Name string
	Attr Attr
}

// Op is one audit-trail record: what a session did to one path, and when.
type Op struct {
	Time   time.Time
	Op     string
	Path   string
	Detail string
}

// Store is the sqlite-backed upper layer over a read-through base. All methods
// are safe for concurrent use.
type Store struct {
	db       *sql.DB
	base     string
	baseRoot *os.Root
	dbPath   string
	policy   func(string) error

	// baseFd is the read-through base's directory descriptor: the base stat
	// and read fast paths answer through it with one syscall, and a negative
	// value means the platform offers no such path, so the store keeps walking
	// os.Root. See TheoryOfBaseFdAccess.
	baseFd int

	mu   sync.Mutex
	read map[string]bool
	// absent caches the paths the upper layer is known not to hold, so the
	// hot read and stat paths answer a path the session never wrote without
	// a sqlite round trip. Any write of the path clears its entry. See
	// TheoryOfUpperAbsentCache.
	absent map[string]bool
	// pending buffers the audit rows not yet written, so a mutation's audit
	// row joins the next batch instead of paying a commit of its own. audit
	// writes the batch when it fills; Ops and Close flush the tail. See
	// TheoryOfAuditBatching.
	pending []Op

	// stmts is the statement set the request path executes. It is a value, not
	// a pointer, so a Store built without preparation holds nil statements and
	// every helper falls back to its SQL text instead of dereferencing nothing.
	// See TheoryOfPreparedStatements.
	stmts preparedStatements
}

// upperAbsent reports whether the upper layer is known not to hold p. A
// missing entry says nothing: the caller must ask the database.
// See TheoryOfUpperAbsentCache.
func (s *Store) upperAbsent(p string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.absent[p]
}

// Open opens the sandbox store at dbPath over the read-through base
// directory. The database file is created when absent. It admits every
// write; see OpenWithPolicy for a store that enforces a write policy.
// See TheoryOfSandboxFilesystem.
func Open(dbPath, base string) (*Store, error) {
	return OpenWithPolicy(dbPath, base, nil)
}

// DefaultDBPath returns the store path of a working directory: one sqlite
// file inside it, so a session's filesystem environment stays with the
// project it belongs to.
func DefaultDBPath(dir string) string {
	return filepath.Join(dir, ".tai-sandbox.db")
}

// Base returns the read-through base directory.
func (s *Store) Base() string {
	return s.base
}

// Close flushes the buffered audit rows, then releases the database and the
// base handles. See TheoryOfAuditBatching.
func (s *Store) Close() error {
	s.flushOps()
	closeBaseDirFd(s.baseFd)
	s.baseFd = -1
	err := s.baseRoot.Close()
	if dbErr := s.db.Close(); err == nil {
		err = dbErr
	}
	return err
}

// OpenWithPolicy opens a sandbox store whose mutations are admitted by
// policy: every write, directory creation, link, rename, mode change, and
// deletion asks policy about its path, and a non-nil result refuses the
// operation before the store changes. A nil policy admits every write,
// which is right for a review process and for tests.
// See TheoryOfWritePolicy.
func OpenWithPolicy(dbPath, base string, policy func(string) error) (*Store, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("sandboxfs: empty database path")
	}
	absDB, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	baseRoot, err := os.OpenRoot(absBase)
	if err != nil {
		return nil, err
	}
	// One read-only descriptor of the base answers the stat and read fast
	// paths. See TheoryOfBaseFdAccess.
	baseFd := openBaseDirFd(absBase)
	dsn := "file:" + url.PathEscape(absDB) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)" +
		// synchronous(NORMAL) keeps every commit durable across an
		// application crash and drops the per-commit fsync a machine crash
		// would need. The sandbox is a session record, reviewed while the
		// session runs: losing the last commits of a machine crash costs
		// nothing a review can use, while an fsync per write and per audit
		// row costs the write path a disk round trip each time.
		"&_pragma=synchronous(NORMAL)" +
		// cache_size(-32000) lifts sqlite's page cache to 32 MiB, so the
		// store's own pages and the audit trail's stay resident instead of
		// being re-read from the database per request.
		"&_pragma=cache_size(-32000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		closeBaseDirFd(baseFd)
		baseRoot.Close()
		return nil, err
	}
	// One connection for the process lifetime: the database may sit inside
	// the filesystem this store serves, and a connection opened later would
	// resolve its own path through that filesystem — a deadlock with the
	// server answering the open. The schema commit below creates the WAL
	// and shared-memory files while they are still reachable.
	// See TheoryOfFuseMount.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.Ping(); err != nil {
		db.Close()
		closeBaseDirFd(baseFd)
		baseRoot.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		closeBaseDirFd(baseFd)
		baseRoot.Close()
		return nil, err
	}
	store := &Store{
		db:       db,
		base:     absBase,
		baseRoot: baseRoot,
		dbPath:   absDB,
		policy:   policy,
		baseFd:   baseFd,
	}
	// The statement set is prepared before the store serves a request, so no
	// request pays a parse or waits for a preparation.
	// See TheoryOfPreparedStatements.
	if err := store.prepareStatements(); err != nil {
		db.Close()
		closeBaseDirFd(baseFd)
		baseRoot.Close()
		return nil, err
	}
	return store, nil
}

// isOwnDBPath reports whether p names the store's own database or one of its
// SQLite siblings. The review session holds the database open for its whole
// life, so a sandbox mutation of that path — or of its -wal and -shm files —
// would corrupt the record the review itself depends on. The comparison is
// textual, because the mount's base view is a bind of a directory: a base
// of "/" makes the sandbox path and the recorded database path identical,
// and a deeper base puts the database outside the sandbox view entirely.
// See TheoryOfSandboxFilesystem.
func (s *Store) isOwnDBPath(p string) bool {
	if s.dbPath == "" {
		return false
	}
	db := filepath.ToSlash(s.dbPath)
	switch Canonical(p) {
	case db, db + "-wal", db + "-shm":
		return true
	}
	return false
}

var (
	// ErrNotExist reports that the path does not exist in the sandbox:
	// neither in the upper layer nor in the base, or hidden by a
	// whiteout.
	ErrNotExist = fs.ErrNotExist
	// ErrExist reports that the path already exists.
	ErrExist = fs.ErrExist
	// ErrNotEmpty reports that a directory still holds entries.
	ErrNotEmpty = syscall.ENOTEMPTY
	// ErrUnsupported reports an operation the store does not implement.
	ErrUnsupported = syscall.ENOTSUP
)

// Canonical returns the clean absolute form of a sandbox path: a leading
// slash, no "." or ".." components, and no trailing slash except the root.
func Canonical(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

func relPath(p string) string {
	r := strings.TrimPrefix(Canonical(p), "/")
	if r == "" {
		return "."
	}
	return r
}

func contentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fifoOriginalHash is the recorded-original sentinel of a base named pipe.
// A pipe carries no content, so its identity is its kind; the sentinel is
// not a hex digest, so it never collides with a file's content hash.
// See TheoryOfCompletePOSIX.
const fifoOriginalHash = "fifo"

// auditBatchSize bounds the audit rows one transaction carries: a batch this
// size pays one commit for its rows and keeps the buffer small enough that a
// process killed outside a close loses at most one batch.
// See TheoryOfAuditBatching.
const auditBatchSize = 64

// maxFileSize bounds one stored file, so a malformed or malicious request never
// makes the store allocate or splice a size the session cannot hold. A grow past
// this bound is refused with EFBIG. The FUSE layer's truncate bound mirrors it,
// and the two must agree: a truncate the mount accepts must be a size the store
// can store. The bound lives here, not beside the wire constants, because the
// store enforces it on its own — a store used without a mount still refuses the
// same grow. See TheoryOfCompletePOSIX.
const maxFileSize = 64 << 20

type fileRow struct {
	path           string
	kind           Kind
	mode           os.FileMode
	mtime          time.Time
	deleted        bool
	size           int64
	target         string
	originalExists bool
	originalHash   string
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFileRow(rs rowScanner) (fileRow, error) {
	var r fileRow
	var kind int
	var mode int64
	var mtime int64
	var deleted int
	var originalExists int
	if err := rs.Scan(
		&r.path, &kind, &mode, &mtime, &deleted, &r.size,
		&r.target, &originalExists, &r.originalHash,
	); err != nil {
		return fileRow{}, err
	}
	r.kind = Kind(kind)
	r.mode = os.FileMode(mode)
	r.mtime = time.Unix(0, mtime)
	r.deleted = deleted != 0
	r.originalExists = originalExists != 0
	return r, nil
}

// row reads one path's full row from the prepared row statement, so a
// mutation's lookup pays no SQL parse. See TheoryOfPreparedStatements.
func (s *Store) row(p string) (fileRow, bool, error) {
	if s.upperAbsent(p) {
		return fileRow{}, false, nil
	}
	r, err := scanFileRow(queryRowStmt(s.db, s.stmts.selectRow, querySelectRow, p))
	if errors.Is(err, sql.ErrNoRows) {
		s.rememberAbsent(p)
		return fileRow{}, false, nil
	}
	if err != nil {
		return fileRow{}, false, err
	}
	return r, true, nil
}

// metaRow reads one path's row: metadata only, because the content lives in the
// chunk table and a write already holds the bytes it is about to store, so a
// read that loaded content would copy a file the write is about to discard. It
// shares the prepared row statement with row, because the column list is the
// same. See TheoryOfChunkedContent and TheoryOfPreparedStatements.
func (s *Store) metaRow(p string) (fileRow, bool, error) {
	if s.upperAbsent(p) {
		return fileRow{}, false, nil
	}
	var r fileRow
	var kind, deleted, originalExists int
	var mode, mtime int64
	err := queryRowStmt(s.db, s.stmts.selectRow, querySelectRow, p).Scan(&r.path, &kind, &mode, &mtime, &deleted, &r.size,
		&r.target, &originalExists, &r.originalHash)
	if errors.Is(err, sql.ErrNoRows) {
		s.rememberAbsent(p)
		return fileRow{}, false, nil
	}
	if err != nil {
		return fileRow{}, false, err
	}
	r.kind = Kind(kind)
	r.mode = os.FileMode(mode)
	r.mtime = time.Unix(0, mtime)
	r.deleted = deleted != 0
	r.originalExists = originalExists != 0
	return r, true, nil
}

// readUpperRange reads one window of a path the upper layer holds. The row's
// metadata — kind, deletion state, and stored size — and the chunk window come
// back in one indexed query, and the window clamps to the stored size, so the
// size column is the authority on length. See TheoryOfReadPath and
// TheoryOfPreparedStatements.
func (s *Store) readUpperRange(p string, off int64, size int) (chunk []byte, kind Kind, found bool, err error) {
	if s.upperAbsent(p) {
		return nil, 0, false, nil
	}
	var k, deleted int
	var stored int64
	err = queryRowStmt(s.db, s.stmts.selectUpper, querySelectUpper, p).Scan(&k, &deleted, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		s.rememberAbsent(p)
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if deleted != 0 {
		return nil, 0, true, ErrNotExist
	}
	if Kind(k) != KindRegular {
		return nil, Kind(k), true, nil
	}
	end := min(off+int64(size), stored)
	if off >= end {
		return []byte{}, Kind(k), true, nil
	}
	chunk, err = s.readChunks(s.db, p, off, int(end-off))
	if err != nil {
		return nil, 0, true, err
	}
	return chunk, Kind(k), true, nil
}

// statRow reads one path's metadata for a stat, from the prepared stat
// statement, so a stat pays no SQL parse. See TheoryOfPreparedStatements.
func (s *Store) statRow(p string) (a Attr, found bool, err error) {
	if s.upperAbsent(p) {
		return Attr{}, false, nil
	}
	var kind, deleted int
	var mode, mtime, size int64
	var targetSize sql.NullInt64
	err = queryRowStmt(s.db, s.stmts.selectStat, querySelectStat, p).Scan(&kind, &mode, &mtime, &deleted, &size, &targetSize)
	if errors.Is(err, sql.ErrNoRows) {
		s.rememberAbsent(p)
		return Attr{}, false, nil
	}
	if err != nil {
		return Attr{}, false, err
	}
	if deleted != 0 {
		return Attr{}, true, ErrNotExist
	}
	a = Attr{
		Kind:    Kind(kind),
		Mode:    os.FileMode(mode),
		ModTime: time.Unix(0, mtime),
	}
	switch a.Kind {
	case KindRegular:
		a.Size = size
	case KindSymlink:
		a.Size = targetSize.Int64
	}
	return a, true, nil
}

// put stores one row's metadata. Content lives in the chunk table: a caller
// that changes content writes it through putContent or the splice, while a row
// that stops being a live regular file drops its chunks here.
// See TheoryOfChunkedContent.
func (s *Store) put(r fileRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.putMetaTx(tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

// SpliceFile writes data at byte offset off of p without reading or rewriting
// the surrounding content, so a write costs bytes proportional to its own
// payload instead of the whole file. A write past the stored end pads the gap
// with zeros, matching the buffer the FUSE layer maintains. A path the upper
// layer holds no live regular row for is materialised first, carrying the base
// content the write starts from; a path the session deleted starts from nothing.
// See TheoryOfWriteSplice.
func (s *Store) SpliceFile(p string, off int64, data []byte, perm os.FileMode) error {
	p = Canonical(p)
	if p == "/" {
		return syscall.EISDIR
	}
	if off < 0 {
		return syscall.EINVAL
	}
	if off > maxFileSize || off+int64(len(data)) > maxFileSize {
		return syscall.EFBIG
	}
	err := s.spliceChunks(p, off, data)
	if errors.Is(err, errNoLiveRegularRow) {
		// No live regular row answered the delta: the path is fresh, a
		// whiteout, or not a regular file. Materialising it either produces
		// the row the splice needs or the error the type deserves, and the
		// splice then runs once more. See TheoryOfWriteSplice.
		if _, err := s.materializeContent(p, -1, perm); err != nil {
			return err
		}
		err = s.spliceChunks(p, off, data)
	}
	if err != nil {
		return err
	}
	s.audit("write", p, fmt.Sprintf("%d bytes at %d", len(data), off))
	return nil
}

// audit records one audit-trail row. Rows accumulate and leave in batches, so
// a burst of mutations shares one commit; Ops and Close flush the tail, so a
// finished session's trail is complete. Audit writes are best-effort: they
// never fail an operation, and a batch that cannot be written is dropped.
// See TheoryOfAuditBatching.
func (s *Store) audit(op, p, detail string) {
	s.mu.Lock()
	s.pending = append(s.pending, Op{
		Time:   time.Now().UTC(),
		Op:     op,
		Path:   p,
		Detail: detail,
	})
	if len(s.pending) < auditBatchSize {
		s.mu.Unlock()
		return
	}
	batch := s.pending
	s.pending = nil
	s.mu.Unlock()
	_ = s.writeOps(batch)
}

// writeOps writes one batch of audit rows in a single transaction: the rows
// share one commit, which is what makes batching worth its buffer. The rows
// keep the buffer's order, so a batch's own chronology survives.
// See TheoryOfAuditBatching.
func (s *Store) writeOps(ops []Op) error {
	if len(ops) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, o := range ops {
		if _, err := tx.Exec(
			`INSERT INTO ops (time, op, path, detail) VALUES (?, ?, ?, ?)`,
			o.Time.Format(time.RFC3339Nano), o.Op, o.Path, o.Detail,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// checkWrite asks the store's write policy about one path. The policy must
// be pure: the FUSE server answers requests on one goroutine, so a policy
// that performed filesystem I/O through the mount would wait for the server
// it is running on. See TheoryOfWritePolicy.
func (s *Store) checkWrite(p string) error {
	if s.policy == nil {
		return nil
	}
	return s.policy(p)
}

// auditRead records the first read of a path in the session. The audit
// question is which files a session touched, so repeated reads add nothing.
func (s *Store) auditRead(p string) {
	s.mu.Lock()
	if s.read == nil {
		s.read = make(map[string]bool)
	}
	if s.read[p] {
		s.mu.Unlock()
		return
	}
	s.read[p] = true
	s.mu.Unlock()
	s.audit("read", p, "")
}

// flushOps writes every buffered audit row. The buffer is detached under the
// lock before the write, so a concurrent audit call keeps its own rows and
// never blocks behind the transaction. See TheoryOfAuditBatching.
func (s *Store) flushOps() {
	s.mu.Lock()
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()
	_ = s.writeOps(pending)
}

// rememberAbsent records that the upper layer does not hold p, so the next
// lookup of the path costs no sqlite round trip. put clears the entry.
// See TheoryOfUpperAbsentCache.
func (s *Store) rememberAbsent(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.absent == nil {
		s.absent = make(map[string]bool)
	}
	s.absent[p] = true
}

// Ops returns the audit trail in insertion order. The buffered tail is
// flushed first, so the answer carries every row the session recorded up to
// this call. See TheoryOfAuditBatching.
func (s *Store) Ops() ([]Op, error) {
	s.flushOps()
	rows, err := s.db.Query(`SELECT time, op, path, detail FROM ops ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Op
	for rows.Next() {
		var at, op, p, detail string
		if err := rows.Scan(&at, &op, &p, &detail); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339Nano, at)
		out = append(out, Op{Time: t, Op: op, Path: p, Detail: detail})
	}
	return out, rows.Err()
}
