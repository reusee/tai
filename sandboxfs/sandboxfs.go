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
shows no change. The recorded hash also marks a change of kind, because a
bare digest cannot: a directory records no hash of its own, so a hash on a
live directory row states that the base held content, and a link's recorded
hash carries a tag, so a regular file whose bytes spell a base link's target
is still a change. Applying a change writes the upper content to the host,
with the mode the row recorded beside it, and drops the upper row, so the
next diff is exact and a repeated apply is a no-op.

The store never targets its own database. The review session holds the
database open for its whole life, and its records are the review surface; a
sandbox mutation of the database file — or of SQLite's -wal and -shm
siblings — would corrupt the record the review depends on. The store
refuses such a mutation before it reads the base and omits the path's net
change, so the path never enters the reviewable set. See
TheoryOfCompletePOSIX for how the file's SQLite siblings arise.

The check matches by file identity, not by text alone. A direct store names
the database and its base as host paths from one tree, so a relative path
between them locates the file's sandbox name. A mounted store serves a
private bind view of the base, whose paths are not the host paths the
database was opened with; the mount records the database's name in the view
so the two names still meet on one inode. Without that step a text
comparison misses every mutation of a database that lives inside the base.

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
file. A read of a written path is two prepared round trips: the row's kind,
deletion state, and stored size, then the chunk window TheoryOfChunkedContent
describes.

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

A directory handle carries the listing snapshot it filled, and every
mutation the mount serves that adds, removes, or renames an entry clears
the snapshot of that directory's open handles. The kernel cannot see a
change this mount makes on its own: FOPEN_CACHE_DIR keeps the listing it
read, and no notify call tells it otherwise, so the server clears its own
cache and the next read rebuilds it from the store, the one source of the
directory's current entries. A continued read keeps the one snapshot its
offsets name, so the rebuild happens only after a mutation, never in the
middle of a listing.
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
not already spend. It is a sync.Map, so a lookup runs without a lock: the read
path consults it on every request, and a shared mutex would serialize every
reader against the first-read audit. The first-read record uses the same form
— one lock-free Load per read, and a LoadOrStore only for the first — because
the audit keeps one record per path, not one per read.
`

const TheoryOfWriteSplice = `
The write path grows with the payload, never with the file. A kernel WRITE
carries at most one negotiated request window of bytes, and the store writes
that window into the chunks the payload covers; the chunk layout and its cost
are TheoryOfChunkedContent's subject. A ten-megabyte file written in
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
statement. A path the upper layer holds no live regular row for refuses the
splice, the caller materialises it, and the splice runs once more; the hot path
therefore costs one commit and never a second lookup of the content this write
exists to avoid copying. The statement shapes are TheoryOfPreparedStatements'.
`

const TheoryOfApplyConflict = `
Write-write conflicts are detected at apply time, against the snapshot the
session computed its changes from. The stored base hash is that snapshot: a
host file whose current content no longer hashes to it changed after the
session read it, and applying would discard the outside change. The same
holds in the other direction for a path the session created, but which now
exists on the host. Such a change is reported with its conflict reason and
never applied silently; the user resolves it. A path the session deleted,
which the host no longer holds, is not a conflict: both sides agree on the
end state, so applying the deletion is a no-op. A deletion whose host path
still matches the snapshot carries the deleted bytes as Original, the same
content a modification reports, so a review shows what the session removed.

A change of kind is a conflict too: the recorded original tells what the
snapshot held, so a host path that now holds a directory where the snapshot
held a file, a link, or a pipe changed outside the session exactly as a
rewritten file did. The tagging that distinguishes the kinds is
TheoryOfSandboxFilesystem's subject.

Detection reads the host, so it belongs to the review step, not to every
read: a session pays nothing for it until it asks what changed or applies.
The hash is content-addressed, so it catches a same-size rewrite that an
mtime comparison would miss.

A permission bit is not content, and no hash sees it: a live path whose
recorded mode differs from the host's own is a change of mode alone, and
applying it sets the bits without replacing the node. The mode-only path
reads the host kind as well as the bytes: a node that is not the regular
file the snapshot recorded is a change of kind and takes the full write,
even when its bytes happen to equal the session's — a base link whose
target string spells a file's content is the case. The recorded mode is
applied as recorded, 000 included: a mode of no bits is a choice, not an
absent one, and a default substituted for it hands the host a mode the
session never made.
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
semantics a filesystem does not route through FUSE. Applying a pipe creates
the node with MKNODAT where the host holds none and sets the recorded bits
on the pipe the host already holds, so an open end survives a mode change. A
read or write addressed to a pipe path reports EINVAL: the store cannot
emulate blocking, and returning content would be worse than an honest
refusal.

A symbolic link reports mode 0o777 on the wire, as Linux does: a link's
permission bits are not its own, and the store's row may carry the bits of
a node the link replaced. Those bits must not leak into the reply. A link
names a target, and an empty target names nothing: the store refuses it with
ENOENT, the answer symlink(2) gives.

Times are recorded, not served: SETATTR's mtime bits replace the stored
mtime, because a toolchain reads mtime to judge staleness, and a caller
that sets the time expects the next read to report it. atime is not part of
the recorded identity and is dropped. A mode change is not a content
change: chmod records the bits and leaves the stored mtime as the base
recorded it, because POSIX gives chmod no mtime effect and a toolchain that
reads mtime to judge staleness must not see a permission change as one.
Rename and a hard link are not content changes either: POSIX gives neither
an mtime effect, so both carry the source's recorded time to the new name,
and a toolchain that renames the parts of a fresh build must not see their
staleness jump.

The mode is authoritative, 000 included, and a recorded mode is applied as
recorded. A create's mode reaches the store after the umask its request
carries: the kernel hands the filesystem both the mode and the mask, and a
filesystem that ignores the mask hands the caller bits the process's umask
withdrew — the same widening a local filesystem never allows. Below that
mask nothing changes the bits: a file with no bits is a file a session
deliberately closed, and a default substituted anywhere on the way — at the
create, the materialisation, the truncate, or the apply — hands the host or
the reader bits the session never chose. A caller that means to keep an
existing mode reads it first, so no layer carries a default to substitute.
The mode also rules what the store can read: a base file whose bits refuse
the process is refused to the first mutation that captures its original,
because that record is the file's whole content hash. Where a local
filesystem changes the name or the bits without reading the bytes, the
store reports the base read's permission error instead, and keeps its
snapshot exact.

Open flags are honoured where the kernel routes them to the filesystem.
CREATE carries O_CREAT, O_EXCL, and O_TRUNC: an existing path is refused
under O_EXCL, kept under O_CREAT alone, and emptied under O_TRUNC. A plain
OPEN with O_TRUNC truncates too — POSIX gives the flag the same effect
whether or not the file is created — and the truncate it performs is a
mutation, so it passes the write policy whatever the access mode: a refused
path reports EACCES at open rather than being emptied behind the policy's
back. A write-mode OPEN and a write-mode CREATE pass the policy first for
the same reason, so the caller sees EACCES from open rather than from its
first write. A create-open that mutates nothing — the path exists, no
O_TRUNC, and the access mode is read-only — asks no permission, because
nothing changes.

Reads and writes cost their own window, never the whole file; the mechanisms
live in TheoryOfReadPath and TheoryOfWriteSplice. A zero-byte write transfers
nothing and does not extend the file, matching Linux. A grow past the size
bound is refused with EFBIG rather than allocating a file the sandbox cannot
hold, and a request's size field is bounded before it becomes a length or an
allocation: a value above the bound is EFBIG, so a malformed claim never
slices a body or reserves memory for bytes that cannot exist. See
TheoryOfFuseMount.

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
reports ENOTDIR, and a destination inside the source reports EINVAL. A
rename whose two names are equal is a no-op on an existing source and
reports ENOENT on a missing one, because rename(2) resolves the source
before it treats the no-op.

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

The access check is the policy's second reader: a probe for W_OK answers
from the policy, so a caller learns at access time what its first write
would report, while a read-only probe stays admitted. The check reads the
path's existence first, so a probe of a deleted path reports ENOENT.

Write confinement is a second barrier, not the review mechanism. A write
the policy admits still lands in the upper layer and still needs the user's
apply, so the policy's job is to keep the store from recording writes the
environment never allowed — the same set the mount layer binds read-write —
and the review step's job is to let the user choose.
`

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

	// baseFd is the read-through base's directory descriptor; a negative value
	// means the platform offers no such path. See TheoryOfBaseFdAccess.
	baseFd int

	// absent caches the paths the upper layer is known not to hold. A write of
	// the path clears its entry. See TheoryOfUpperAbsentCache.
	absent sync.Map // p string -> bool

	// read records the paths the session already audited a read of, so the
	// trail keeps one record per path. See TheoryOfAuditBatching.
	read sync.Map // p string -> bool

	// mu guards pending, the audit rows not yet written.
	mu      sync.Mutex
	pending []Op

	// stmts is the statement set the request path executes. It is a value, so a
	// Store built without preparation holds nil statements and every helper
	// falls back to its SQL text. See TheoryOfPreparedStatements.
	stmts preparedStatements
}

// upperAbsent reports whether the upper layer is known not to hold p. A
// missing entry says nothing: the caller must ask the database.
// See TheoryOfUpperAbsentCache.
func (s *Store) upperAbsent(p string) bool {
	_, ok := s.absent.Load(p)
	return ok
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
// SQLite siblings. See TheoryOfSandboxFilesystem.
func (s *Store) isOwnDBPath(p string) bool {
	if s.dbPath == "" {
		return false
	}
	cp := Canonical(p)
	db := filepath.ToSlash(s.dbPath)
	switch cp {
	case db, db + "-wal", db + "-shm":
		return true
	}
	rel, err := filepath.Rel(s.base, s.dbPath)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	var own []os.FileInfo
	for _, name := range [3]string{rel, rel + "-wal", rel + "-shm"} {
		if fi, err := s.baseRoot.Stat(name); err == nil {
			own = append(own, fi)
		}
	}
	if len(own) == 0 {
		return false
	}
	fi, err := s.baseRoot.Stat(relPath(cp))
	if err != nil {
		return false
	}
	for _, o := range own {
		if os.SameFile(fi, o) {
			return true
		}
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
// See TheoryOfSandboxFilesystem and TheoryOfCompletePOSIX.
const fifoOriginalHash = "fifo"

// symlinkOriginalPrefix tags the recorded-original of a base symbolic link.
// See TheoryOfSandboxFilesystem.
const symlinkOriginalPrefix = "symlink:"

// auditBatchSize bounds the audit rows one transaction carries. See
// TheoryOfAuditBatching.
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
// mutation's lookup pays no SQL parse. The scan reads metadata only: content
// lives in the chunk table, and a write already holds the bytes it is about to
// store, so a read that assembled content would copy a file the write is about
// to discard. See TheoryOfChunkedContent and TheoryOfPreparedStatements.
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

// readUpperRange reads one window of a path the upper layer holds, clamped to
// the stored size. See TheoryOfReadPath and TheoryOfPreparedStatements.
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

func (s *Store) SpliceFile(p string, off int64, data []byte, perm os.FileMode) error {
	p = Canonical(p)
	if p == "/" {
		return syscall.EISDIR
	}
	if off < 0 {
		return syscall.EINVAL
	}
	// A zero-byte write transfers nothing and does not extend the file, so it
	// must not reach the size guard, which would raise the stored length to
	// the offset. See TheoryOfCompletePOSIX.
	if len(data) == 0 {
		return nil
	}
	if err := s.checkWrite(p); err != nil {
		return err
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

// audit records one audit-trail row. Audit writes are best-effort: they never
// fail an operation, and a batch that cannot be written is dropped.
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

// writeOps writes one batch of audit rows in a single transaction, keeping the
// buffer's order. See TheoryOfAuditBatching and TheoryOfPreparedStatements.
func (s *Store) writeOps(ops []Op) error {
	if len(ops) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, o := range ops {
		if _, err := runStmt(
			tx, s.stmts.putOp, queryPutOp,
			o.Time.Format(time.RFC3339Nano), o.Op, o.Path, o.Detail,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// checkWrite asks the store's write policy about one path, and refuses the
// store's own database up front, so a stale row can never reach a write that
// skips the capture that also refuses it.
// See TheoryOfWritePolicy and TheoryOfSandboxFilesystem.
func (s *Store) checkWrite(p string) error {
	if s.isOwnDBPath(p) {
		return fmt.Errorf("%w: the sandbox database is never a sandbox target: %s", os.ErrPermission, p)
	}
	if s.policy == nil {
		return nil
	}
	return s.policy(p)
}

// auditRead records the first read of a path in the session, so the trail keeps
// one record per path instead of one per read.
// See TheoryOfAuditBatching.
func (s *Store) auditRead(p string) {
	if _, ok := s.read.Load(p); ok {
		return
	}
	if _, loaded := s.read.LoadOrStore(p, true); loaded {
		return
	}
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
// lookup of the path costs no sqlite round trip. A write of the path clears
// the entry. See TheoryOfUpperAbsentCache.
func (s *Store) rememberAbsent(p string) {
	s.absent.Store(p, true)
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
