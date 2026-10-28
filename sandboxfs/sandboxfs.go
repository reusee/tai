// Package sandboxfs gives a task its own filesystem environment: a
// read-through union of the host filesystem and an upper layer that records
// every mutation, serves the session's reads, lists the net changes, and
// applies selected changes back to the host. The upper layer is process
// memory, a SQL record file, or a directory record of ordinary files; see
// TheoryOfUpperLayer. See TheoryOfSandboxFilesystem.
package sandboxfs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const TheoryOfSandboxFilesystem = `
A task runs inside one filesystem environment built from two layers. The base
layer is the host filesystem, read through unchanged, so the model reads real
data without copying it into the sandbox. The upper layer records every
mutation: content, kind, mode, and the base state of the path as it stood when
the session first touched it. The upper layer has three backends — process
memory, which is the default and leaves nothing behind; a SQL record file that
outlives the process and that the review command browses and applies later; and
a directory record of ordinary files that outlives the process the same way,
with the host filesystem doing the storage. The record file is a sqlite
database, or a DuckDB database on request; see TheoryOfSqlDialects. See
TheoryOfUpperLayer, TheoryOfSqlUpper and TheoryOfDirUpper.

The boundary is the filesystem, not the tool's file abstraction. Change blocks
keep applying atomically through changes.FileStore, and FileStore never learns
that the sandbox exists: the sandbox intercepts below it, at the mount
namespace, so it covers every reader and writer — the toolchain, shell
commands, and any other subprocess — and not only the change-block path.

Reads fall through to the base except where the upper layer overrides the path;
a whiteout hides a deleted base file, and a directory listing merges both
layers. Every mutation writes an audit row, and a path's first read writes one
too: the audit answers which files a session touched, not how often it read
them.

A path's net change is derived from hashes, never from a stored second copy of
the content: the first mutation stores the base content hash, so a path
rewritten to its original bytes — or created and deleted in one session — shows
no change. The recorded hash also marks a change of kind, because a bare digest
cannot: a directory records no hash of its own, so a hash on a live directory
row states that the base held content, and a link's recorded hash carries a tag,
so a regular file whose bytes spell a base link's target is still a change.
Applying a change writes the upper content to the host, with the mode the row
recorded beside it, and drops the upper row, so the next diff is exact and a
repeated apply is a no-op.

The store never targets its own record. The review session holds the record open
for its whole life, and its records are the review surface; a sandbox mutation
of the record — the database file, one of its sibling files (SQLite's -wal and
-shm, DuckDB's .wal), or, for a directory record, any file beneath the record
directory — would corrupt the record the review depends on. The store refuses
such a mutation before it reads the base and omits the path's net change, so the
path never enters the reviewable set. A memory backend has no record to
protect, so the check finds nothing. See TheoryOfCompletePOSIX for how the
SQL record's siblings arise, and TheoryOfDirRecord for the directory record's
own extent.

The check matches by file identity, not by text alone. A direct store names the
record and its base as host paths from one tree, so a relative path between them
locates the file's sandbox name. A mounted store serves a private bind view of
the base, whose paths are not the host paths the record was opened with; the
mount records the record's name in the view so the two names still meet on one
inode. Without that step a text comparison misses every mutation of a record
that lives inside the base. A directory record makes the match structural: its
sandbox name and every path beneath it are the record.

The environment is deliberately not a fresh isolated copy. The model must reach
the host's real data; the safety comes from intercepting writes and making them
reviewable, not from hiding data. This is also why a plain overlay filesystem
does not serve: it offers no read or write hook to audit, intercept, or defer a
write, while this upper layer offers all three.
`

const TheoryOfReadPath = `
The read path is the hot path of a coding task: tooling stats, lists, and
reads thousands of paths while a session writes a few dozen. Every
optimization here leaves the write path untouched.

A listing of a session that has written nothing costs no merge. The base is the
only layer that holds an entry, so the listing is the base's own entries:
no map, no name slice, and no second pass to overlay rows that cannot exist.
A store that holds a row takes the merged path, so a listing and a stat still
agree on one path. The cursor is the one implementation of the walk and the
merge, so the mount's windows and the store's own whole-listing API can never
disagree on which entries exist. See TheoryOfUpperLayer and
TheoryOfDirectoryStream.

A listing carries "." and ".." from the store, and the directory's own
attribute is not read twice to supply them: the kind check a listing already
pays produces the attribute the "." entry reports, the parent's name is a prefix
of the directory's own, and the parent's attributes cost one stat. A tree walk
therefore pays per directory one stat of the directory, one of its parent, and
the base's own enumerated window — never a second stat of a directory the same
request already stat'ed.

A metadata lookup never loads content. Stat needs the kind, mode, mtime,
and size; the row's metadata is the whole answer, and the content lives in the
upper layer's content store or, for a metadata-only row, in the base. The size
therefore comes from the row's stored size column or, when that column carries
the metadata sentinel, from one Lstat of the base path, so a getattr or lookup
on a large file costs one small row and never a heap copy of the file.

A read handle opens the base file once. The kernel streams a file in
windows; reopening and re-checking the host file per window spends a stat
and an open on data the mount already made readable. The handle keeps the
descriptor it opened, and the host type is checked once, before the open,
so a pipe is never opened and its read never blocks.

The cache never shadows the session. Every read asks the upper layer
first — a SQL layer that holds no row answers without a query, a memory
layer answers from its map, and a directory layer from one failed file open —
so a written path serves its new content and a whiteout reports the deletion
without any invalidation. A truncation — the truncating open or setattr's size
change — marks every reader cached for the path stale, and a write drops its
own handle's reader and marks every other handle's, because the content that
was just written is the store content from then on. The mark is an atomic flag
the reader consumes before it serves a window, never a lock the invalidator
waits on: the invalidator can be another handle's writer, and two writers
waiting for each other would deadlock. The cached descriptor fixes a base
file's view at open, the same snapshot the review step compares the host
against when it reports a conflict.

A directory handle carries the entries its replies already named and a cursor
for the rest, so one reply pulls only the window it carries and a large
directory never costs one long request (see TheoryOfDirectoryStream). A
mutation the mount serves marks the handle stale, and the effect depends on
where the kernel reads next: a read from the first entry reseeds the listing
from the store, so a re-listing sees the session's own change, while a
continued read keeps the stream it started with, so a walk never skips or
repeats a name because its own offsets shifted. The kernel cannot see a change
this mount makes on its own — FOPEN_CACHE_DIR keeps the listing it read, and
no notify call tells it otherwise — which is why the stale mark is the
server's own notice.
`

const TheoryOfWriteSplice = `
The write path grows with the payload, never with the file. A kernel WRITE
carries at most one negotiated request window of bytes, and the upper layer
writes that window and no other byte: the SQL and memory layers write the
chunks the payload covers, and the directory layer writes its one content file
at the window's offset (see TheoryOfDirUpper). The chunk layout and its cost
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

The materialisation is the one step two concurrent first writes can both
reach, so the upper layer re-checks its own premise inside its own atomic
step: the row is written only while the path holds no live regular row that
already carries content. A row another writer created first is that writer's
content, and replacing it would drop the bytes that writer already stored.
The loser leaves the winner's row alone and answers with it, so the caller's
delta lands on the content that exists.

The size sentinel separates the two cases the guard must tell apart. A
committed metadata-only row records a chmod, a utimes, or a revived whiteout:
no content write produced it, so the row is exactly what a materialisation
replaces, and treating it as a competing writer would strand every splice
into a path the session only touched by metadata. A committed row with a
stored length is the competing writer; the sentinel means the guard's premise
still holds.

The splice runs in one atomic step whose first statement both guards the row
and extends its size, so the delta's admissibility and its length cost one
statement. A path the upper layer holds no live regular row for refuses the
splice, the caller materialises it, and the splice runs once more; the hot path
therefore costs one commit and never a second lookup of the content this write
exists to avoid copying. The memory and SQL layers' statement shapes are
TheoryOfPreparedStatements'.
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

Durability: FSYNC and FSYNCDIR succeed without doing anything, and they mean
exactly what the backend means: the SQL layer's writes are transactions that
persist as they complete, whichever dialect holds the record; the directory
layer's writes are files that reach the operating system as they complete
(see TheoryOfDirUpper); and the memory layer's writes live only in this
process. A tool that fsyncs to force data to disk gets the answer it expects;
with either durable layer the data has already left the process by the time
the call returns.

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
it serves: a policy that opened a path through the mount would wait on the
request it is itself answering, so the worker serving that request could
never finish (see TheoryOfFuseConcurrency). The writable set is therefore
resolved once before the mount and matched by path prefix, not by resolving
symlinks per write.

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
buffered by the store and handed to the upper layer in batches: audit appends
a row and writes the buffer when it reaches auditBatchSize, and Ops and Close
flush the tail, so one commit serves a whole batch.

A commit dominates a small statement's cost in the SQL layer, so a row
written per commit charged every mutation and every path's first read a
commit of its own beside the operation's own statement. Batching removes that
second commit from the request path without changing what the trail records.
The directory layer pays one append per batch instead of one per row, and the
memory layer pays nothing per row; both keep the same shape, because the batch
is the store's, not the backend's.

The trade-off is the tail: rows buffered when a process dies outside a close
are lost, so the trail is complete for a session that ends normally and may be
short for one that was killed. The audit is best-effort by design — it never
fails an operation — and the review step reads changes from the content rows,
which stay durable at every write in the durable layers. Rows keep their call
order within a batch; under concurrent mutation a later batch can commit
before an earlier one, so the trail is ordered by insertion, and each batch
keeps its own order.
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

// Store is one sandbox's filesystem state: the read-through base and the upper
// layer that records the session's mutations. The union logic lives here and
// is written once; the upper layer decides only where a row and its content
// are kept. See TheoryOfSandboxFilesystem and TheoryOfUpperLayer.
type Store struct {
	upper    upperStore
	base     string
	baseRoot *os.Root
	dbPath   string
	policy   func(string) error

	// baseFd is the read-through base's directory descriptor; a negative value
	// means the platform offers no such path. See TheoryOfBaseFdAccess.
	baseFd int

	// read records the paths the session already audited a read of, so the
	// trail keeps one record per path. See TheoryOfAuditBatching.
	read sync.Map // p string -> bool

	// mu guards pending, the audit rows not yet written.
	mu      sync.Mutex
	pending []Op
}

// Open opens the sandbox store at dbPath over the read-through base
// directory, with the sqlite dialect of the SQL upper layer. The database
// file is created when absent. It admits every write; see OpenWithPolicy for
// a store that enforces a write policy, OpenDuckDB for a record held by
// another engine, and OpenDir for a directory record. See
// TheoryOfSandboxFilesystem, TheoryOfSqlUpper, TheoryOfDirRecord and
// TheoryOfSqlDialects.
func Open(dbPath, base string) (*Store, error) {
	return OpenWithPolicy(dbPath, base, nil)
}

// DefaultDBPath returns the store path of a working directory: one record
// file inside it — a sqlite database, or a DuckDB database when the mount
// selects that dialect — so a durable session's filesystem environment stays
// with the project it belongs to. A session on the default memory layer leaves
// no file here. The result is a filesystem path, so it joins with the
// platform's separator even though the store itself speaks slash paths.
// See TheoryOfUpperLayer and TheoryOfSqlDialects.
func DefaultDBPath(dir string) string {
	return filepath.Join(dir, ".tai-sandbox.db")
}

// Base returns the read-through base directory.
func (s *Store) Base() string {
	return s.base
}

// Close flushes the buffered audit rows, then releases the upper layer and the
// base handles. See TheoryOfAuditBatching.
func (s *Store) Close() error {
	s.flushOps()
	closeBaseDirFd(s.baseFd)
	s.baseFd = -1
	err := s.baseRoot.Close()
	if upperErr := s.upper.close(); err == nil {
		err = upperErr
	}
	return err
}

// OpenWithPolicy opens a sandbox store with the sqlite dialect of the SQL upper
// layer, whose mutations are admitted by policy: every write, directory
// creation, link, rename, mode change, and deletion asks policy about its
// path, and a non-nil result refuses the operation before the store changes. A
// nil policy admits every write, which is right for a review process and for
// tests. See TheoryOfWritePolicy and TheoryOfSqlUpper.
func OpenWithPolicy(dbPath, base string, policy func(string) error) (*Store, error) {
	return openDurable(dbPath, base, policy, openSQLiteUpper)
}

// openDurable assembles a sandbox store on the durable SQL upper layer a
// constructor opens: the two dialects differ only in which open function the
// caller passes, so the empty-path check, the absolute path, and the store
// assembly live here once. See TheoryOfSqlDialects.
func openDurable(dbPath, base string, policy func(string) error, open func(string) (*sqlUpper, error)) (*Store, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("sandboxfs: empty database path")
	}
	absDB, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	upper, err := open(absDB)
	if err != nil {
		return nil, err
	}
	return newStore(base, upper, policy)
}

// OpenDuckDB opens the sandbox store at dbPath over the read-through base
// directory with the DuckDB dialect of the SQL upper layer. The binary must
// import a DuckDB driver; this package links none, so a binary without one
// reports the open error and names the dialect. It admits every write; see
// OpenDuckDBWithPolicy for a store that enforces a write policy. See
// TheoryOfSqlDialects.
func OpenDuckDB(dbPath, base string) (*Store, error) {
	return OpenDuckDBWithPolicy(dbPath, base, nil)
}

// OpenDuckDBWithPolicy opens a DuckDB-backed sandbox store whose mutations are
// admitted by policy. See TheoryOfWritePolicy and OpenDuckDB.
func OpenDuckDBWithPolicy(dbPath, base string, policy func(string) error) (*Store, error) {
	return openDurable(dbPath, base, policy, openDuckDBUpper)
}

// OpenAny opens the sandbox store at dbPath by routing the record to the layer
// and dialect that wrote it: a record directory opens the directory layer,
// DuckDB opens when the file's header carries DuckDB's magic, and sqlite opens
// for sqlite's header, for a path that does not exist yet, and for any other
// shape, whose own error then names a file that is no record. The review
// command takes this path, so a user never has to remember which layer wrote
// the record. The DuckDB route needs the binary's driver import, exactly as the
// mount does. See TheoryOfDirRecord and TheoryOfSqlDialects.
func OpenAny(dbPath, base string) (*Store, error) {
	if dbPath != "" {
		if info, err := os.Stat(dbPath); err == nil && info.IsDir() {
			return OpenDir(dbPath, base)
		}
		engine, err := recordEngine(dbPath)
		if err != nil {
			return nil, err
		}
		if engine == duckDBDialect.name {
			return OpenDuckDB(dbPath, base)
		}
	}
	return Open(dbPath, base)
}

// recordEngine reads the engine name a record file declares by its header: the
// magic a dialect names. An absent file, an unreadable header, or a header that
// names neither engine returns sqlite, because the default record is a sqlite
// database and an unrecognized file's own error is the honest report of a path
// that is no sandbox record. See TheoryOfSqlDialects.
func recordEngine(dbPath string) (string, error) {
	f, err := os.Open(dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sqliteDialect.name, nil
		}
		return "", err
	}
	defer f.Close()
	var header [16]byte
	n, _ := f.Read(header[:])
	if n == 0 {
		return sqliteDialect.name, nil
	}
	magic := string(header[:n])
	switch {
	case strings.HasPrefix(magic, duckDBDialect.magic):
		return duckDBDialect.name, nil
	case strings.HasPrefix(magic, sqliteDialect.magic):
		return sqliteDialect.name, nil
	}
	return sqliteDialect.name, nil
}

// OpenMemory opens a sandbox store over the read-through base directory with
// the memory upper layer: the session's mutations live in process memory, so
// the session leaves no file behind and nothing survives the process. It
// admits every write; see OpenMemoryWithPolicy for a store that enforces a
// write policy. See TheoryOfUpperLayer.
func OpenMemory(base string) (*Store, error) {
	return OpenMemoryWithPolicy(base, nil)
}

// OpenMemoryWithPolicy opens a memory-backed sandbox store whose mutations are
// admitted by policy. See TheoryOfWritePolicy and TheoryOfUpperLayer.
func OpenMemoryWithPolicy(base string, policy func(string) error) (*Store, error) {
	return newStore(base, newMemoryUpper(), policy)
}

// newStore assembles one store around an upper layer: the base directory is
// opened read-through, the base descriptor answers the read fast path, and the
// upper layer reports its own record file, when it has one, so the store never
// mutates the record it serves from. See TheoryOfBaseFdAccess and
// TheoryOfSandboxFilesystem.
func newStore(base string, upper upperStore, policy func(string) error) (*Store, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		_ = upper.close()
		return nil, err
	}
	baseRoot, err := os.OpenRoot(absBase)
	if err != nil {
		_ = upper.close()
		return nil, err
	}
	s := &Store{
		upper:    upper,
		base:     absBase,
		baseRoot: baseRoot,
		policy:   policy,
		baseFd:   openBaseDirFd(absBase),
	}
	if sb, ok := upper.(selfBackedUpper); ok {
		s.dbPath = sb.backingPath()
	}
	return s, nil
}

// isOwnDBPath reports whether p names the upper layer's own record: the record
// path itself, a backend's sibling file beside it (sqlite's -wal and -shm,
// DuckDB's .wal), and — for a directory record — every path beneath the record
// directory. A memory layer has no record, so every path is free. The record's
// path is a filesystem path and a sandbox path is a slash path, so the name
// comparison runs on slash form twice: once against the recorded path, and once
// against the path relative to the base, which is the record's sandbox name. The
// second form is what catches a directory record's files after a mount rewrote
// the record's path to its private base view, and what catches a direct store
// whose record sits inside its own base. The identity comparison below then
// covers a mount whose base view renamed the file. See TheoryOfSandboxFilesystem,
// TheoryOfDirRecord and TheoryOfSqlDialects.
func (s *Store) isOwnDBPath(p string) bool {
	if s.dbPath == "" {
		return false
	}
	cp := Canonical(p)
	db := filepath.ToSlash(s.dbPath)
	switch cp {
	case db, db + "-wal", db + "-shm", db + ".wal":
		return true
	}
	rel, err := filepath.Rel(s.base, s.dbPath)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	// The record's sandbox name and, for a record directory, every path under
	// it are the record. A directory record holds a metadata file per recorded
	// path and a content file per content-bearing path, all beneath this name.
	// See TheoryOfDirRecord.
	record := Canonical(filepath.ToSlash(rel))
	switch {
	case cp == record:
		return true
	case record != "/" && strings.HasPrefix(cp, record+"/"):
		return true
	}
	var own []os.FileInfo
	for _, name := range [4]string{rel, rel + "-wal", rel + "-shm", rel + ".wal"} {
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
	if p[0] != '/' {
		p = "/" + p
	}
	return path.Clean(p)
}

// relPath returns the base-relative form of an arbitrary sandbox path. The
// hot paths clean their input once at entry and derive the relative form with
// canonicalRel, so no request cleans the same path twice.
// See TheoryOfBaseFdAccess.
func relPath(p string) string {
	return canonicalRel(Canonical(p))
}

// canonicalRel returns the base-relative form of a path that is already
// canonical: a stat or a read cleans its input once at entry, and cleaning it
// again for the base access would repeat the traversal on every request.
func canonicalRel(p string) string {
	r := p[1:]
	if r == "" {
		return "."
	}
	return r
}

// parentOf returns the parent of a canonical absolute path. The parent's
// bytes are a prefix of the path's own, so naming it costs no allocation:
// a canonical path has no trailing slash except the root's, and the last
// slash is the component separator. path.Dir would build the same string
// from a fresh buffer on every listing and every ".." resolution.
// See TheoryOfFuseMount.
func parentOf(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i <= 0 {
		return "/"
	}
	return p[:i]
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

// auditBatchSize bounds the audit rows one batch carries. See
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

// row reads one path's full row from the upper layer. It is a thin forwarder
// so the union logic reads the same whether the row comes from a database, a
// map, or a record file. See TheoryOfUpperLayer.
func (s *Store) row(p string) (fileRow, bool, error) {
	return s.upper.row(p)
}

// readUpperRange reads one window of a path the upper layer holds, clamped to
// the stored size. A metadata-only row reports found false, so the caller
// serves the window from the base. See TheoryOfBaseCapture and
// TheoryOfUpperLayer.
func (s *Store) readUpperRange(p string, off int64, size int) ([]byte, Kind, bool, error) {
	return s.upper.window(p, off, size)
}

// statRow reads one path's metadata for a stat. A metadata-only row reports the
// base's own length, which costs one Lstat and never the file's content. See
// TheoryOfBaseCapture and TheoryOfReadPath.
func (s *Store) statRow(p string) (Attr, bool, error) {
	r, ok, err := s.upper.row(p)
	if err != nil || !ok {
		return Attr{}, false, err
	}
	if r.deleted {
		return Attr{}, true, ErrNotExist
	}
	a := attrOfRow(r)
	if r.kind == KindRegular && r.size < 0 {
		// The row records metadata only, so the base holds the length.
		// See TheoryOfBaseCapture.
		if info, statErr := s.baseRoot.Lstat(relPath(p)); statErr == nil {
			a.Size = info.Size()
		}
	}
	return a, true, nil
}

// put stores one row's metadata in the upper layer. Content lives in the
// layer's content store: a caller that changes content writes it through
// putContent, while a row that stops being a live regular file drops its
// content. See TheoryOfChunkedContent.
func (s *Store) put(r fileRow) error {
	return s.upper.putRow(r)
}

// putContent stores one row's metadata and its whole content in one atomic
// step of the upper layer.
func (s *Store) putContent(r fileRow, content []byte) error {
	return s.upper.replaceContent(r, content)
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
	err := s.upper.splice(p, off, data, time.Now())
	if errors.Is(err, errNoLiveRegularRow) {
		// No live regular row answered the delta: the path is fresh, a
		// whiteout, or not a regular file. Materialising it either produces
		// the row the splice needs or the error the type deserves, and the
		// splice then runs once more. See TheoryOfWriteSplice.
		if _, err := s.materializeContent(p, -1, perm); err != nil {
			return err
		}
		err = s.upper.splice(p, off, data, time.Now())
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
	_ = s.upper.writeAudit(batch)
}

// checkWrite asks the store's write policy about one path, and refuses the
// upper layer's own record up front, so a stale row can never reach a write
// that skips the capture that also refuses it. See TheoryOfWritePolicy and
// TheoryOfSandboxFilesystem.
func (s *Store) checkWrite(p string) error {
	if s.isOwnDBPath(p) {
		return fmt.Errorf("%w: the sandbox record is never a sandbox target: %s", os.ErrPermission, p)
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
// never blocks behind the write. See TheoryOfAuditBatching.
func (s *Store) flushOps() {
	s.mu.Lock()
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()
	_ = s.upper.writeAudit(pending)
}

// Ops returns the audit trail in application order. The buffered tail is
// flushed first, so the answer carries every row the session recorded up to
// this call. See TheoryOfAuditBatching.
func (s *Store) Ops() ([]Op, error) {
	s.flushOps()
	return s.upper.auditRows()
}
