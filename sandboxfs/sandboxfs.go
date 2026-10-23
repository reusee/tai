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

The read path streams: a read serves the requested window from sqlite's
substr over the stored blob, or from the base file's own ReadAt, so a large
dependency costs one sized response rather than a whole-file allocation.
The write path materializes: the first write of a handle loads the file,
applies the delta, and writes the whole content back, because sqlite has no
in-place splice. A zero-byte write transfers nothing and does not extend
the file, matching Linux. Truncation reads only the retained prefix and
pads with zeros; a grow past 64 MB is refused with EFBIG rather than
allocating a file the sandbox cannot hold, and the same bound refuses a
write whose offset would overflow. See TheoryOfFuseMount.

Two handles over one path both materialize their own buffer, so a write
through one invalidates the other's buffer: the next write through the
other handle reloads the store's now-current content before applying its
delta, and preserves both writes. Truncation invalidates every handle over
the path, because the caller's next read must report the new size; the
writing handle keeps its buffer after its own write, because the content it
just wrote is the store content a reload would return.

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

const schema = `
CREATE TABLE IF NOT EXISTS files (
    path TEXT PRIMARY KEY,
    kind INTEGER NOT NULL,
    mode INTEGER NOT NULL,
    mtime INTEGER NOT NULL,
    deleted INTEGER NOT NULL DEFAULT 0,
    content BLOB,
    target TEXT NOT NULL DEFAULT '',
    original_exists INTEGER NOT NULL DEFAULT 0,
    original_hash TEXT NOT NULL DEFAULT ''
);
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

// Store is the sqlite-backed upper layer over a read-through base. All
// methods are safe for concurrent use.
type Store struct {
	db       *sql.DB
	base     string
	baseRoot *os.Root
	dbPath   string
	policy   func(string) error

	mu   sync.Mutex
	read map[string]bool
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

// Close releases the database and the base handle.
func (s *Store) Close() error {
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
	dsn := "file:" + url.PathEscape(absDB) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
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
		baseRoot.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		baseRoot.Close()
		return nil, err
	}
	return &Store{
		db:       db,
		base:     absBase,
		baseRoot: baseRoot,
		dbPath:   absDB,
		policy:   policy,
	}, nil
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

type fileRow struct {
	path           string
	kind           Kind
	mode           os.FileMode
	mtime          time.Time
	deleted        bool
	content        []byte
	target         string
	originalExists bool
	originalHash   string
}

func (r fileRow) attr() Attr {
	a := Attr{
		Kind:    r.kind,
		Mode:    r.mode,
		ModTime: r.mtime,
	}
	switch r.kind {
	case KindRegular:
		a.Size = int64(len(r.content))
	case KindSymlink:
		a.Size = int64(len(r.target))
	}
	return a
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
		&r.path, &kind, &mode, &mtime, &deleted,
		&r.content, &r.target, &originalExists, &r.originalHash,
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

func (s *Store) row(p string) (fileRow, bool, error) {
	r, err := scanFileRow(s.db.QueryRow(
		`SELECT path, kind, mode, mtime, deleted, content, target, original_exists, original_hash
		 FROM files WHERE path = ?`, p))
	if errors.Is(err, sql.ErrNoRows) {
		return fileRow{}, false, nil
	}
	if err != nil {
		return fileRow{}, false, err
	}
	return r, true, nil
}

func (s *Store) allRows() ([]fileRow, error) {
	rows, err := s.db.Query(
		`SELECT path, kind, mode, mtime, deleted, content, target, original_exists, original_hash
		 FROM files ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fileRow
	for rows.Next() {
		r, err := scanFileRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) put(r fileRow) error {
	var deleted int
	if r.deleted {
		deleted = 1
	}
	var originalExists int
	if r.originalExists {
		originalExists = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO files (path, kind, mode, mtime, deleted, content, target, original_exists, original_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		   kind = excluded.kind,
		   mode = excluded.mode,
		   mtime = excluded.mtime,
		   deleted = excluded.deleted,
		   content = excluded.content,
		   target = excluded.target,
		   original_exists = excluded.original_exists,
		   original_hash = excluded.original_hash`,
		r.path, int(r.kind), int64(r.mode), r.mtime.UnixNano(), deleted,
		r.content, r.target, originalExists, r.originalHash,
	)
	return err
}

// audit appends one audit-trail row. Audit writes are best-effort: they
// never fail an operation.
func (s *Store) audit(op, p, detail string) {
	_, _ = s.db.Exec(
		`INSERT INTO ops (time, op, path, detail) VALUES (?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), op, p, detail,
	)
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

// Ops returns the audit trail in application order.
func (s *Store) Ops() ([]Op, error) {
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
