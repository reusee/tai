//go:build linux

package sandboxfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const TheoryOfFuseMount = `
The sandbox is mounted as a FUSE filesystem, not emulated in-process. FUSE
is the only mechanism that routes every file operation of every process —
the toolchain, shell commands, the change-block applier — through one
userspace handler, so the store's audit and its upper layer cover all I/O
without each consumer knowing they exist. A userspace-only abstraction would
cover only the code paths that opt into it, which defeats the design.

The implementation speaks the kernel protocol directly instead of a third-
party library: the protocol is small (a request header, an opcode, and
opcode-specific fixed bodies), the kernel ABI is stable, and one dependency
fewer keeps the sandbox inside the module's existing constraints. One reader
takes requests from the device and a fixed set of workers answers them; the
concurrent model, its locking, and the bound on requests in flight live in
TheoryOfFuseConcurrency. The request path is still bounded in the bytes it
moves and the rows it writes, so a request's duration never grows with the
size of a file or a directory the session happens to touch (see
TheoryOfBaseCapture) — and concurrency is what keeps one request that does
run long from becoming the whole process's stall.

The read loop separates the errors that do not end the daemon from the ones
that do. EINTR says a signal arrived. ENOENT says the caller interrupted its
request before this server read it, and the answer is to read the next
request. Returning on either stops the server, and from then on every
filesystem call through the mount fails with EIO.

A request body carries its fixed header followed by its names. A body
shorter than the fixed header, or a name with no terminator, is malformed:
the handler reports EINVAL rather than slicing past the body, because a
panic in a handler kills the daemon for every later request. The
decodeStruct padding covers the fixed fields only; the names come from the
raw length.

The request header's length field is checked before the body is sliced. The
request buffer is reused, so a length that claims bytes the read did not
deliver would hand the handler another request's bytes. A length below
the header or above the bytes read is EINVAL; a zero length leaves the
whole read as the body.

Node ids are paths. The protocol requires stable node ids, and the store is
path-addressed, so the server allocates numeric ids on first use and keeps
the mapping until the process exits. Paths are the identity; numeric ids
are only the wire form. LOOKUP of a fresh path allocates a new node id, and
LOOKUP of a known path returns the existing id. A listing resolves every
entry it carries under one acquisition of the node table and names each
entry by appending to the canonical parent, because a directory walk is the
toolchain's dominant pattern and a lock and a path cleaning per entry are
the costs that pattern repeats for a large tree. FORGET is accepted without a
reply and ignored: paths stay addressable for the session's life, matching
the store, which keeps every touched path in the audit trail and, for a
durable session, its record.

Write-back happens on WRITE, not on flush: the store is the session's only
copy of what it wrote, so an open file's content lives in the store and a
kernel WRITE records the new content immediately. A killed process therefore
leaves a consistent store, and the review step sees every byte the session
wrote. The read and write paths own the mechanism (see TheoryOfReadPath and
TheoryOfWriteSplice); the POSIX-visible rules are in TheoryOfCompletePOSIX.

A request buffer is sized for the largest request the kernel may send: the
announced MaxWrite payload after the header and the write body. Sizing the
buffer at MaxWrite exactly truncates the payload of a maximal WRITE, which
is the shape a toolchain emits for every full block of a large file.

Passthrough directories bypass FUSE entirely. See TheoryOfPassthrough.
`

const TheoryOfFuseConcurrency = `
Request concurrency theory:
- One reader takes requests from the device and a fixed set of workers
  answers them. A single goroutine makes every request's duration the whole
  process's stall: the kernel holds the requests of every process in the
  sandbox behind the one being answered, so a request that runs long — a
  first write materialising a base file, a directory rename walking a
  subtree, a hash of an untouched file — stops every filesystem operation of
  the process until it finishes. The pool removes that single point.
- A session that touches many paths meets those long requests often: every
  walk over a large tree, every first write of a large file, and every
  subtree rename is one chance to land behind one. The same workload on a
  small tree meets few, which is why a large project stalls and a directory
  of a few files seldom does. The pool turns each meeting into one worker's
  cost instead of the process's.
- The pool is bounded by its buffers. The reader takes a buffer from the free
  set before it reads, so the number of requests in flight bounds itself,
  memory stays fixed whatever the workload asks for, and a wedged request
  costs one worker rather than the process. A request's buffer is recycled
  once its reply is written, because the reply payload is read from it.
- Two replies never interleave. Each reply is one write of one header and one
  body, and the request's unique id names the reply it answers, so workers
  answer out of order without corruption.
- Handle-scoped requests — read, write, both listings, and release — share
  one handle's state: the read window buffer, the listing window and the
  entries already delivered, the cached base reader, and the stale marks. One
  lock per handle serializes them and holds through the reply write, because
  the reply payload aliases that state: releasing it sooner would let the next
  request on the handle overwrite bytes the kernel is about to read.
- The lock is per handle, never per server. A server-wide lock makes one
  file's request every other file's wait, and the long requests are exactly
  the hot ones: a first write to a base file hashes the file's whole prefix
  and materialises it into chunks before it stores a byte of the delta, and
  behind that one request every read of every unrelated source file would
  queue. A large project issues thousands of reads per write, which is why it
  meets the stall and a directory of a few files seldom does. Per handle, the
  same write costs the readers of one file.
- Path-scoped requests — lookup, getattr, setattr, open, create, mkdir,
  unlink, rename, link, symlink, readlink, access, statfs, init, mknod —
  hold no handle state and run concurrently with each other and with a slow
  handle request.
- Invalidation is the one step that reaches into handles it does not hold. A
  mutation marks an open listing stale under that directory handle's own
  lock, taken one handle at a time with the node table released first, so the
  two locks never nest. A cached base reader is marked stale through an
  atomic flag instead, and the reader consumes the flag under its own lock
  before it serves a window: the writer that invalidates must never wait for
  another handle's lock, because two writers to one path would then each wait
  for the other.
- The store is safe for concurrent use: sqlite's handle is, the statement set
  is, the audit buffer and the absence cache and the node table carry their
  own locks. Paths stay addressable, so two workers naming one path agree on
  its node id.
- FORGET, BATCH_FORGET, and INTERRUPT expect no reply and get none, on
  whatever worker reads them.
`

const TheoryOfInitNegotiation = `
The FUSE INIT handshake is the first and cheapest performance lever: it sets the
per-request ceiling every later file operation runs under, before a single byte
moves.

The handshake also names the protocol version. This server speaks major 7, so
a kernel that names a newer major is told to fall back to 7 and reopens the
handshake at that version; a kernel that names an older major speaks a
protocol this server cannot serve, and the negotiation fails with EPROTO. The
minor is capped the same way: a newer kernel is answered at the last field
definition this server knows.

BIG_WRITES dominates the write path. Without it the kernel caps one WRITE at a
single page, so a toolchain writing a multi-megabyte file sends one request per
page, and each request pays its own sqlite transaction and commit: the chunked
store keeps the bytes linear, but the per-request cost is what multiplies.
MAX_PAGES raises the announced window to the 1 MiB the kernel caps max_write
at, so the same write costs one request, one transaction, and one commit.

READDIRPLUS carries a fuse_entry_out per directory entry, so the kernel needs no
LOOKUP after a listing: a directory walk — the toolchain's dominant pattern —
becomes one request instead of one plus one per entry.

The read side has two more levers. A reply that leaves max_readahead at zero
turns the kernel's readahead off, and every file read becomes one request per
page; the reply echoes the kernel's own window, so a streamed file arrives as
a few large requests. Readahead reads are background requests, so the echo
buys nothing while the background queue is too shallow to hold them:
max_background and congestion_threshold are announced far above the server's
worker count on purpose — the queue exists to keep every worker supplied, and
its depth costs nothing while the workers are idle (see
TheoryOfFuseConcurrency).

The remaining bits cost one round trip each: ATOMIC_O_TRUNC folds the truncating
open's SETATTR into the OPEN the caller already issues, ASYNC_READ keeps several
reads in flight, and PARALLEL_DIROPS lifts the kernel's serialization of
independent directory operations. Writeback caching is deliberately absent: it
would defer writes past the open that checks the write policy.

Every bit is echoed only when the kernel offers it, so an older kernel keeps the
conservative window this server announced before the negotiation existed.
`

const TheoryOfAttrCaching = `
The kernel's attribute and entry timeouts are the cheapest performance lever a
FUSE filesystem has. For the length of the timeout the kernel answers STAT and
LOOKUP from its own cache, so a toolchain that stats the same dependency a
thousand times sends one request instead of a thousand, and the win multiplies
every later optimization because those requests never reach the store at all.

The store's own semantics make a long timeout safe. Every mutation the session
performs goes through this mount, and the kernel drops the cached attributes of
a path whose content, size, or times a request changes; the session therefore
never reads stale metadata that it wrote itself. A host change the sandbox
cannot see is, by design, the snapshot the session works from: the base reader
fixes a file's view at open, and a host edit surfaces as a conflict at apply
time rather than as a change the session must observe mid-read. A short timeout
would buy freshness the session does not want and pay one round trip per stat
to get it.

The entry timeout and the attribute timeout move together. An entry that stays
valid keeps its node id, so a repeated path resolution costs neither a LOOKUP
nor a new node id, and the server's node table stops growing with the number of
times a toolchain walked the tree.

A rendered attribute also reads the process identity. The uid and gid cannot
change during the process's life, so resolving them once at first use keeps two
syscalls out of every attribute a lookup, a getattr, or a listing entry renders
— and a READDIRPLUS reply renders one attribute per directory entry, which
turns a hundred-entry listing into two hundred syscalls paid for a constant.
`

const TheoryOfOpenCaching = `
The kernel caches file pages and directory entries per open, and an open reply
can ask it to keep them across opens. Both flags matter here for the reason
TheoryOfAttrCaching gives: the base layer is a snapshot the session works
from, so a page the kernel keeps is still the page the store would serve, and
the session's own mutations invalidate what they touch.

Keeping the page cache turns a repeated read of an unchanged dependency into a
kernel memory copy: the request never reaches the store, and the read path
stops paying one request per page.

A write-mode open does not ask for the page cache. Its content is the
session's to change, and a page kept from before the write would shadow the
bytes the write just stored. A truncating open is a content change whatever
its access mode, so it declines the page cache on the same ground.
`

// fuseCacheTimeoutSec is the kernel's attribute and entry cache lifetime in
// seconds. See TheoryOfAttrCaching.
const fuseCacheTimeoutSec = 60

const TheoryOfPassthrough = `
Passthrough directories are mounted over the sandbox view, not served
through the store: the Go toolchain caches (GOCACHE, GOMODCACHE, GOPATH/pkg)
and the user config directory. The design calls these directories valueless
for review — their content is machine-managed, and their traffic dominates
the write volume of a task — so intercepting them would add cost without
adding review value. /proc, /sys, and /dev are also passthroughs, because
they must be real kernel filesystems: the container needs PID-scoped /proc,
device nodes, and sysfs, none of which a read-through file proxy can
emulate.

/tmp is special. It is local and unreviewed like the others, but the
filesystem's mountpoint lives inside the container's /tmp, so binding the
container's /tmp would expose the mount to its own tree: a recursive walk
would enter the filesystem again through the passthrough it is serving.
/tmp therefore receives a fresh tmpfs instead of a bind. The mountpoint
still lives on the container's /tmp, so it leaves no directory in the
project, and the fresh tmpfs gives the sandbox the same isolated, in-memory
/tmp the bind would have offered.

The passthrough list is resolved before the root pivot, because after the
pivot a path refers to the sandbox view the store serves and no longer
reaches the host. Each entry is mounted onto its place inside the FUSE
mount before the pivot; after the pivot the path reaches the host directory
directly. A passthrough whose host directory is absent is skipped before its
target is created, so a skip leaves no directory behind for the session to
review.

The kernel filesystems are bound whatever the caller passes. A caller that
omits /dev is the failure the rule exists to prevent: the store would serve
a character device, report it as a regular file, and open it to hash its
content — a terminal blocks until a key arrives, and the read consumes what
it waited for — and the blocked request holds its worker for as long as the
terminal stays silent. The caller's own entries ride beside them, and a
duplicate binds once.

A durable session's record is the one thing that must not transit FUSE: the
FUSE daemon serves the store, so a store access that reached the daemon
would recurse. The store therefore opens the record, and its backend's
files, before the pivot: a database record's -wal and -shm siblings come
with its first commit, and a directory record is one directory handle taken
on the host. The default record sits inside the working directory, which the
mount serves, so this ordering is what keeps the record off the request
path, not an optimization. The mount helper documents the requirement on
MountOptions.DBPath, and the store refuses every mutation of the record so
the review's source stays intact (see TheoryOfSandboxFilesystem).
`

const (
	// fuseRootID is the node id the kernel assigns to the filesystem root.
	fuseRootID = 1

	fuseMaxWrite = 1 << 17

	// fuseProtocolMinor is the kernel protocol minor we speak: field
	// definitions through Linux 7.31.
	fuseProtocolMinor = 31
)

// fuseProtocolMajor is the kernel protocol major this server speaks.
// See TheoryOfInitNegotiation.
const fuseProtocolMajor = 7

// Linux FUSE opcodes, matching include/uapi/linux/fuse.h.
const (
	fuseOpLookup      = 1
	fuseOpForget      = 2
	fuseOpGetattr     = 3
	fuseOpSetattr     = 4
	fuseOpReadlink    = 5
	fuseOpSymlink     = 6
	fuseOpMknod       = 8
	fuseOpMkdir       = 9
	fuseOpUnlink      = 10
	fuseOpRmdir       = 11
	fuseOpRename      = 12
	fuseOpLink        = 13
	fuseOpOpen        = 14
	fuseOpRead        = 15
	fuseOpWrite       = 16
	fuseOpStatfs      = 17
	fuseOpRelease     = 18
	fuseOpFsync       = 20
	fuseOpFlush       = 25
	fuseOpInit        = 26
	fuseOpOpendir     = 27
	fuseOpReaddir     = 28
	fuseOpReleasedir  = 29
	fuseOpAccess      = 34
	fuseOpCreate      = 35
	fuseOpInterrupt   = 36
	fuseOpDestroy     = 38
	fuseOpBatchForget = 42
	fuseOpRename2     = 45
)

// fuseOpFsyncdir is FUSE_FSYNCDIR, the directory counterpart of FSYNC that
// tooling issues after creating or renaming entries: opcode 30, between
// FUSE_RELEASEDIR 29 and FUSE_GETLK 31. The neighbouring 32 is FUSE_SETLK, a
// byte-range lock this store does not implement and answers as ENOSYS.
// See TheoryOfCompletePOSIX.
const fuseOpFsyncdir = 30

// fuseWorkerCount is the number of request workers. One worker would make
// every request's duration the whole process's stall, and a pool wider than
// the store's own concurrency buys nothing while it multiplies the request
// buffers the pool holds. See TheoryOfFuseConcurrency.
const fuseWorkerCount = 4

const (
	fuseInitAsyncRead       = 1 << 0  // several reads in flight
	fuseInitAtomicOTrunc    = 1 << 3  // OPEN honours O_TRUNC
	fuseInitBigWrites       = 1 << 5  // WRITE above one page
	fuseInitDoReaddirplus   = 1 << 13 // READDIRPLUS implemented
	fuseInitReaddirplusAuto = 1 << 14 // kernel picks per directory
	fuseInitParallelDirops  = 1 << 18 // parallel directory operations
	fuseInitMaxPages        = 1 << 22 // max_pages is valid

	// fuseInitWanted is the capability set the server asks for: the request
	// ceilings FUSE_MAX_PAGES and BIG_WRITES buy, the LOOKUP savings of
	// READDIRPLUS, and the one-round-trip bits.
	fuseInitWanted = fuseInitAsyncRead |
		fuseInitAtomicOTrunc |
		fuseInitBigWrites |
		fuseInitDoReaddirplus |
		fuseInitReaddirplusAuto |
		fuseInitParallelDirops |
		fuseInitMaxPages

	// fusePageSize is the unit FUSE_MAX_PAGES counts its page budget in.
	fusePageSize = 4096

	// fuseMaxWriteMax is the largest payload the server announces when the
	// kernel negotiates FUSE_MAX_PAGES: 1 MiB, the ceiling the kernel itself
	// caps max_write at.
	fuseMaxWriteMax = 1 << 20

	// fuseMaxPages is the page count covering fuseMaxWriteMax.
	fuseMaxPages = fuseMaxWriteMax / fusePageSize

	fuseMaxBackground       = 256
	fuseCongestionThreshold = 192

	// fuseInHeaderSize and fuseOutHeaderSize are the fixed wire sizes of the
	// request and response headers, read directly so the hot path pays no
	// reflection cost.
	fuseInHeaderSize  = 40
	fuseOutHeaderSize = 16

	// fuseOpReaddirplus is FUSE_READDIRPLUS: READDIR carrying a fuse_entry_out
	// ahead of every dirent, so a listing needs no LOOKUP per entry.
	fuseOpReaddirplus = 44

	// fuseDirentSize is the fixed part of one fuse_dirent; the name follows,
	// padded to an 8-byte boundary.
	fuseDirentSize = 24
)

// FATTR_* bits of fuse_setattr_in.valid.
const (
	fattrMode     = 1 << 0
	fattrSize     = 1 << 3
	fattrAtime    = 1 << 4
	fattrMtime    = 1 << 5
	fattrAtimeNow = 1 << 7
	fattrMtimeNow = 1 << 8
)

// fuseInHeader mirrors struct fuse_in_header.
type fuseInHeader struct {
	Len    uint32
	Opcode uint32
	Unique uint64
	Nodeid uint64
	Uid    uint32
	Gid    uint32
	Pid    uint32
	Pad    uint32
}

// fuseOutHeader mirrors struct fuse_out_header.
type fuseOutHeader struct {
	Len    uint32
	Error  int32
	Unique uint64
}

// fuseAttr mirrors struct fuse_attr.
type fuseAttr struct {
	Ino       uint64
	Size      uint64
	Blocks    uint64
	Atime     uint64
	Mtime     uint64
	Ctime     uint64
	Atimensec uint32
	Mtimensec uint32
	Ctimensec uint32
	Mode      uint32
	Nlink     uint32
	Uid       uint32
	Gid       uint32
	Rdev      uint32
	Blksize   uint32
	Flags     uint32
}

// fuseEntryOut mirrors struct fuse_entry_out.
type fuseEntryOut struct {
	Nodeid         uint64
	Generation     uint64
	EntryValid     uint64
	AttrValid      uint64
	EntryValidNsec uint32
	AttrValidNsec  uint32
	Attr           fuseAttr
}

// fuseAttrOut mirrors struct fuse_attr_out.
type fuseAttrOut struct {
	AttrValid     uint64
	AttrValidNsec uint32
	Pad           uint32
	Attr          fuseAttr
}

// fuseInitIn mirrors struct fuse_init_in.
type fuseInitIn struct {
	Major        uint32
	Minor        uint32
	MaxReadahead uint32
	Flags        uint32
	Flags2       uint32
	Pad          uint32
}

// fuseInitOut mirrors struct fuse_init_out through protocol 7.31, plus the
// 7.36 flags2 field the kernel reads at the same offset: major, minor,
// max_readahead, flags, max_background, congestion_threshold, max_write,
// time_gran, then the 7.28 max_pages field, the 7.31 map_alignment field,
// the 7.36 flags2 field, and the unused tail. The field order is the wire
// order the kernel parses by its own struct offsets. A max_pages wider than
// 16 bits pushes map_alignment and flags2 two bytes late, and the kernel
// reads an alignment and a flag set the server never announced; the two
// fields' zero defaults hide that until one of them is set. A field whose
// negotiating flag is absent stays zero, which the kernel ignores.
// See TheoryOfFuseMount and TheoryOfInitNegotiation.
type fuseInitOut struct {
	Major               uint32
	Minor               uint32
	MaxReadahead        uint32
	Flags               uint32
	MaxBackground       uint16
	CongestionThreshold uint16
	MaxWrite            uint32
	TimeGran            uint32
	MaxPages            uint16
	MapAlignment        uint16
	Flags2              uint32
	Unused              [7]uint32
}

// fuseOpenIn mirrors struct fuse_open_in.
type fuseOpenIn struct {
	Flags uint32
	Pad   uint32
}

// fuseOpenOut mirrors struct fuse_open_out.
type fuseOpenOut struct {
	Fh        uint64
	OpenFlags uint32
	Pad       uint32
}

// fuseReadIn mirrors struct fuse_read_in, shared by READ and READDIR.
type fuseReadIn struct {
	Fh        uint64
	Offset    uint64
	Size      uint32
	ReadFlags uint32
	LockOwner uint64
	Flags     uint32
	Pad       uint32
}

// fuseAccessIn mirrors struct fuse_access_in: the access mask the kernel asks
// about. The reply carries no body; only the error code answers.
type fuseAccessIn struct {
	Mask uint32
	Pad  uint32
}

// fuseWriteIn mirrors struct fuse_write_in; the payload follows the header.
type fuseWriteIn struct {
	Fh         uint64
	Offset     uint64
	Size       uint32
	WriteFlags uint32
	LockOwner  uint64
	Flags      uint32
	Pad        uint32
}

// fuseWriteOut mirrors struct fuse_write_out.
type fuseWriteOut struct {
	Size uint32
	Pad  uint32
}

// fuseReleaseIn mirrors struct fuse_release_in.
type fuseReleaseIn struct {
	Fh           uint64
	Flags        uint32
	ReleaseFlags uint32
	LockOwner    uint64
}

// fuseFlushIn mirrors struct fuse_flush_in.
type fuseFlushIn struct {
	Fh        uint64
	Pad       uint32
	Pad2      uint32
	LockOwner uint64
}

// fuseFsyncIn mirrors struct fuse_fsync_in.
type fuseFsyncIn struct {
	Fh         uint64
	FsyncFlags uint32
	Pad        uint32
}

// fuseCreateIn mirrors struct fuse_create_in; the name follows the header.
type fuseCreateIn struct {
	Flags uint32
	Mode  uint32
	Umask uint32
	Pad   uint32
}

// fuseCreateOut mirrors struct fuse_create_out.
type fuseCreateOut struct {
	Entry fuseEntryOut
	Open  fuseOpenOut
}

// fuseMkdirIn mirrors struct fuse_mkdir_in; the name follows the header.
type fuseMkdirIn struct {
	Mode  uint32
	Umask uint32
}

// fuseMknodIn mirrors struct fuse_mknod_in; the name follows the header.
type fuseMknodIn struct {
	Mode  uint32
	Rdev  uint32
	Umask uint32
	Pad   uint32
}

// fuseLinkIn mirrors struct fuse_link_in; the new name follows the header.
type fuseLinkIn struct {
	Oldnodeid uint64
}

// fuseSetattrIn mirrors struct fuse_setattr_in.
type fuseSetattrIn struct {
	Valid     uint32
	Pad       uint32
	Fh        uint64
	Size      uint64
	LockOwner uint64
	Atime     uint64
	Mtime     uint64
	Ctime     uint64
	Atimensec uint32
	Mtimensec uint32
	Ctimensec uint32
	Mode      uint32
	Unused4   uint32
	Uid       uint32
	Gid       uint32
	Unused5   uint32
}

// fuseRenameIn mirrors struct fuse_rename_in.
type fuseRenameIn struct {
	Newdir uint64
}

// fuseRename2In mirrors struct fuse_rename2_in.
type fuseRename2In struct {
	Newdir uint64
	Flags  uint32
	Pad    uint32
}

// fuseForgetIn mirrors struct fuse_forget_in.
type fuseForgetIn struct {
	Nlookup uint64
}

// fuseStatfsOut mirrors struct fuse_statfs_out.
type fuseStatfsOut struct {
	Blocks  uint64
	Bfree   uint64
	Bavail  uint64
	Files   uint64
	Ffree   uint64
	Bsize   uint32
	Namelen uint32
	Frsize  uint32
	Pad     uint32
	Spare   [6]uint32
}

// fuseDirent mirrors the fixed part of struct fuse_dirent; the name follows,
// padded to an 8-byte boundary.
type fuseDirent struct {
	Ino     uint64
	Off     uint64
	Namelen uint32
	Type    uint32
}

type fuseHandle struct {
	// mu serializes this handle's requests through their reply write. The lock
	// is per handle, never per server: a slow request on one file must not
	// stall a request on another. See TheoryOfFuseConcurrency.
	mu sync.Mutex

	path  string
	isDir bool

	// entries holds the directory entries this handle's replies already named,
	// and the kernel's reply offset is an index into it: a continued read
	// resumes where it stopped and a rewind reads the same entries again. dir
	// is the cursor the entry past the last one comes from, nil once the
	// listing is exhausted, so one request pulls only the entries it replies
	// with. dirStale reports that a mutation through this mount changed the
	// directory, so a read from the first entry reseeds the listing while a
	// continued read keeps the stream it started with.
	// See TheoryOfDirectoryStream.
	entries  []DirEntry
	dir      *dirCursor
	dirStale bool

	// dirWindow holds the entries one listing reply carries, reused across
	// requests so a reply costs no allocation after its first.
	// See TheoryOfDirectoryStream.
	dirWindow []DirEntry

	// mode is the file's permission bits, captured at open or create, so the
	// write path needs no metadata lookup of its own: the open that handed
	// the handle back already read the attribute. See TheoryOfWriteSplice.
	mode os.FileMode

	// readBuf is the handle's read window buffer, reused across reads: the
	// handle's requests are serialized through their reply write, so a
	// streamed file costs one buffer per handle instead of one allocation per
	// window. See TheoryOfReadPath and TheoryOfFuseConcurrency.
	readBuf []byte

	// listBuf is the directory listing's reply scratch, reused across
	// READDIR and READDIRPLUS calls for the same reason readBuf is reused:
	// the handle's requests are serialized, so a repeated listing of one
	// directory allocates nothing after its first call. Both handlers size it
	// from the entries that fit instead of the announced window, which can be
	// far larger than a small directory.
	// See TheoryOfInitNegotiation.
	listBuf []byte

	// base is the handle's cached base-layer reader: opened lazily on the
	// first read the upper layer does not answer, closed on release or
	// invalidation. See TheoryOfReadPath.
	base *os.File

	// baseStale marks base stale after a write or a truncation of the path.
	// The mark is atomic because the invalidator may be another handle's
	// writer or a path-scoped request, and it must never wait for this
	// handle's lock: two writers to one path would deadlock if each waited for
	// the other. Only the request holding mu consumes the mark, before it
	// serves a window. See TheoryOfFuseConcurrency and TheoryOfReadPath.
	baseStale atomic.Bool
}

// closeDir releases the handle's listing cursor, if any.
// See TheoryOfDirectoryStream.
func (h *fuseHandle) closeDir() {
	if h.dir != nil {
		h.dir.Close()
		h.dir = nil
	}
}

// closeBase releases the handle's cached base-layer reader, if any. It leaves
// the stale mark alone: the mark belongs to whoever set it, and a reader that
// cleared it here would wipe a mark a concurrent writer set in the gap between
// its Swap and this call, leaving the handle on a descriptor that fixes a view
// the store has replaced. Only the reader consumes the mark, with Swap, and
// only release drops the handle that carried it. The handle's requests are
// serialized through their reply write, so no read is in flight while it runs.
// See TheoryOfReadPath and TheoryOfFuseConcurrency.
func (h *fuseHandle) closeBase() {
	if h.base != nil {
		_ = h.base.Close()
		h.base = nil
	}
}

type fuseServer struct {
	store *Store
	devFd int

	// readReq reads one request from the FUSE device. It is a field so a test
	// can drive the request loop without a kernel mount and inject the errors
	// the device reports, above all the ENOENT of an interrupted request.
	// See TheoryOfFuseMount.
	readReq func([]byte) (int, error)

	// beforeDispatch runs at the top of dispatch, before the request takes any
	// lock. It is a field so a test can hold one request open and prove that
	// the other workers keep answering. See TheoryOfFuseConcurrency.
	beforeDispatch func(opcode uint32)

	// afterLock runs once a handle-scoped request holds its own handle's lock,
	// before the handler touches that handle's state. It is a field so a test
	// can hold one file's request open and prove that another file's request
	// still answers; a server-wide lock would make the second wait for the
	// first. See TheoryOfFuseConcurrency.
	afterLock func(opcode uint32, nodeid uint64)

	mu      sync.Mutex
	nodes   map[uint64]string
	byPath  map[string]uint64
	handles map[uint64]*fuseHandle
	nextID  uint64
	nextFh  uint64

	// timings aggregates the measured cost of every answered request, so a
	// session can report which operation its workload spends its time in.
	// The constructor installs it and the read loop stamps every request; a
	// nil value measures nothing, which is what a server built without the
	// constructor gets. See TheoryOfOperationTiming.
	timings *OpTimings

	done chan struct{}
}

// fuseRequest is one request the reader handed to the pool: a pooled buffer
// and the length of the request in it. The buffer returns to the pool once the
// reply is written. See TheoryOfFuseConcurrency.
type fuseRequest struct {
	buf []byte
	n   int
}

func newFuseServer(store *Store, devFd int) *fuseServer {
	s := &fuseServer{
		store:   store,
		devFd:   devFd,
		nodes:   map[uint64]string{fuseRootID: "/"},
		byPath:  map[string]uint64{"/": fuseRootID},
		handles: make(map[uint64]*fuseHandle),
		nextID:  fuseRootID + 1,
		nextFh:  1,
		timings: newOpTimings(),
		done:    make(chan struct{}),
	}
	// The request read goes through the device descriptor the mount opened.
	// A test replaces it to drive the loop from a script and to inject the
	// errors the device reports. See TheoryOfFuseMount.
	s.readReq = func(buf []byte) (int, error) {
		return unix.Read(devFd, buf)
	}
	// The flight window's report names the path behind a slow request, so the
	// aggregate resolves a node id through the server's node table. Rendering
	// happens outside the aggregate's lock and the resolver takes the node
	// table's lock, so the two never nest. See TheoryOfFlightRecorder.
	s.timings.resolve = s.pathName
	return s
}

func (s *fuseServer) run() {
	defer close(s.done)
	// The buffers, the request queue, and the workers are the request pool:
	// the reader takes a buffer before it reads, so the number of requests in
	// flight bounds itself and memory stays fixed whatever the workload asks
	// for. See TheoryOfFuseConcurrency.
	size := fuseInHeaderSize + binary.Size(fuseWriteIn{}) + fuseMaxWriteMax
	free := make(chan []byte, fuseWorkerCount)
	for i := 0; i < fuseWorkerCount; i++ {
		free <- make([]byte, size)
	}
	requests := make(chan fuseRequest, fuseWorkerCount)
	var wg sync.WaitGroup
	for i := 0; i < fuseWorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for req := range requests {
				// One start stamp and one record per request attribute the
				// server's own cost — dispatch plus the reply write — to the
				// request's opcode, and the request joins the flight ring the
				// moment it finishes.
				// See TheoryOfOperationTiming and TheoryOfFlightRecorder.
				opcode := binary.LittleEndian.Uint32(req.buf[4:8])
				nodeid := binary.LittleEndian.Uint64(req.buf[16:24])
				mark := s.timings.markRequest(nodeid)
				s.dispatch(req.buf[:req.n])
				s.timings.recordRequest(opcode, mark)
				free <- req.buf
			}
		}()
	}
	for {
		buf := <-free
		n, err := s.readReq(buf)
		if err != nil {
			free <- buf
			// Two read errors do not end the daemon. EINTR says a signal
			// arrived. ENOENT says the caller interrupted its request before
			// this server read it; the FUSE device asks for the next request.
			// Returning on either kills the daemon and every later filesystem
			// call fails with EIO. See TheoryOfFuseMount.
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.ENOENT) {
				continue
			}
			break
		}
		if n <= 0 || n < fuseInHeaderSize {
			free <- buf
			break
		}
		requests <- fuseRequest{buf: buf, n: n}
	}
	// The queued requests drain before the server reports itself stopped, so
	// Unmount observes every answered request's timing.
	close(requests)
	wg.Wait()
}

func (s *fuseServer) dispatch(buf []byte) {
	if len(buf) < fuseInHeaderSize {
		return
	}
	h := fuseInHeader{
		Len:    binary.LittleEndian.Uint32(buf[0:4]),
		Opcode: binary.LittleEndian.Uint32(buf[4:8]),
		Unique: binary.LittleEndian.Uint64(buf[8:16]),
		Nodeid: binary.LittleEndian.Uint64(buf[16:24]),
	}
	if s.beforeDispatch != nil {
		s.beforeDispatch(h.Opcode)
	}
	// The header's Len field is the request's own length. A length shorter
	// than the header or longer than the bytes read describes a body that is
	// not there, and the reused request buffer still holds another request's
	// bytes beyond the read: parsing them would answer a request the kernel
	// never sent. A zero length means the sender left the field empty, so the
	// whole read stands as the body. See TheoryOfFuseMount.
	end := len(buf)
	if h.Len != 0 {
		if h.Len < fuseInHeaderSize || int(h.Len) > len(buf) {
			if h.Unique != 0 {
				s.respond(h.Unique, syscall.EINVAL, nil)
			}
			return
		}
		end = int(h.Len)
	}
	body := buf[fuseInHeaderSize:end]
	// Every handle-scoped request names its handle in the first field of its
	// body — READ and READDIR, WRITE, and both releases put the handle number
	// at offset zero — so the dispatch takes that one handle's lock and holds
	// it through the reply write, whose payload aliases the handle's state.
	// The lock is per handle: a server-wide one would make one file's slow
	// request every other file's wait. See TheoryOfFuseConcurrency.
	if isHandleScoped(h.Opcode) && len(body) >= 8 {
		s.mu.Lock()
		held := s.handles[binary.LittleEndian.Uint64(body[0:8])]
		s.mu.Unlock()
		if held != nil {
			held.mu.Lock()
			defer held.mu.Unlock()
			if s.afterLock != nil {
				s.afterLock(h.Opcode, h.Nodeid)
			}
		}
	}
	var respBody []byte
	var err error
	switch h.Opcode {
	case fuseOpInit:
		respBody, err = s.handleInit(body)
	case fuseOpDestroy:
		respBody = nil
	case fuseOpLookup:
		respBody, err = s.handleLookup(h.Nodeid, body)
	case fuseOpGetattr:
		respBody, err = s.handleGetattr(h.Nodeid)
	case fuseOpSetattr:
		respBody, err = s.handleSetattr(h.Nodeid, body)
	case fuseOpMknod:
		respBody, err = s.handleMknod(h.Nodeid, body)
	case fuseOpOpen:
		respBody, err = s.handleOpen(h.Nodeid, body)
	case fuseOpRead:
		respBody, err = s.handleRead(body)
	case fuseOpWrite:
		respBody, err = s.handleWrite(body)
	case fuseOpRelease:
		respBody, err = s.handleRelease(body)
	case fuseOpFlush, fuseOpFsync:
		respBody, err = nil, nil
	case fuseOpFsyncdir:
		// A directory's metadata lives in sqlite, which persists every
		// write; the fsyncdir durability guarantee is already satisfied.
		// See fuseOpFsyncdir.
		respBody, err = nil, nil
	case fuseOpOpendir:
		respBody, err = s.handleOpendir(h.Nodeid)
	case fuseOpReaddir:
		respBody, err = s.handleReaddir(body)
	case fuseOpReaddirplus:
		respBody, err = s.handleReaddirPlus(body)
	case fuseOpReleasedir:
		respBody, err = s.handleRelease(body)
	case fuseOpCreate:
		respBody, err = s.handleCreate(h.Nodeid, body)
	case fuseOpMkdir:
		respBody, err = s.handleMkdir(h.Nodeid, body)
	case fuseOpUnlink:
		respBody, err = s.handleUnlink(h.Nodeid, body, false)
	case fuseOpRmdir:
		respBody, err = s.handleUnlink(h.Nodeid, body, true)
	case fuseOpRename:
		respBody, err = s.handleRename(h.Nodeid, body, false)
	case fuseOpRename2:
		respBody, err = s.handleRename(h.Nodeid, body, true)
	case fuseOpSymlink:
		respBody, err = s.handleSymlink(h.Nodeid, body)
	case fuseOpReadlink:
		respBody, err = s.handleReadlink(h.Nodeid)
	case fuseOpLink:
		respBody, err = s.handleLink(h.Nodeid, body)
	case fuseOpStatfs:
		respBody, err = encodeStruct(&fuseStatfsOut{
			Blocks: 1, Bfree: 1, Bavail: 1, Files: 1024, Ffree: 1024,
			Bsize: 4096, Namelen: 255, Frsize: 4096,
		}), nil
	case fuseOpAccess:
		// FUSE_ACCESS declares no output argument: the kernel's request
		// carries none, so the reply body must be empty. A body here fails
		// the kernel's reply-length check, aborts the connection with
		// ENODEV, and every later filesystem call — the chdir into the
		// mount root first — returns EIO. See TheoryOfFuseMount.
		respBody, err = s.handleAccess(h.Nodeid, body)
	case fuseOpForget, fuseOpBatchForget, fuseOpInterrupt:
		// No reply is expected for FORGET, BATCH_FORGET, or INTERRUPT.
		return
	default:
		respBody, err = nil, syscall.ENOSYS
	}
	if h.Unique == 0 {
		return
	}
	s.respond(h.Unique, err, respBody)
}

// isHandleScoped reports whether one opcode reads or writes per-handle state
// or replies with a payload that aliases it, so its request must hold the
// handle lock through its reply write. See TheoryOfFuseConcurrency.
func isHandleScoped(opcode uint32) bool {
	switch opcode {
	case fuseOpRead, fuseOpWrite, fuseOpReaddir, fuseOpReaddirplus,
		fuseOpRelease, fuseOpReleasedir:
		return true
	}
	return false
}

// respond writes one reply. The header and the payload leave as one writev:
// a read reply can carry a whole negotiated request window, and copying that
// payload behind the header would move the bytes a second time per read. The
// kernel consumes one write per reply, and writev is one write, so concurrent
// workers never interleave two replies. The header is a fixed array on the
// worker's stack, because a shared scratch would let one worker overwrite a
// reply another is writing. See TheoryOfFuseMount and
// TheoryOfFuseConcurrency.
func (s *fuseServer) respond(unique uint64, err error, body []byte) {
	errno := int32(0)
	if err != nil {
		errno = fuseErrno(err)
		body = nil
	}
	var hdr [fuseOutHeaderSize]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(fuseOutHeaderSize+len(body)))
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(errno))
	binary.LittleEndian.PutUint64(hdr[8:16], unique)
	if len(body) == 0 {
		_, _ = unix.Write(s.devFd, hdr[:])
		return
	}
	_, _ = unix.Writev(s.devFd, [][]byte{hdr[:], body})
}

// fuseErrno maps an error to the negative errno the kernel expects in a
// response header. A syscall.Errno carries its own code and wins over the
// sentinel checks: syscall.EPERM also satisfies errors.Is(err, os.ErrPermission),
// so testing the sentinels first would report EACCES where the operation
// reported EPERM — mknod of a device node is the case that surfaced it. The
// two sentinels that are not errno values are fs.ErrNotExist and fs.ErrExist;
// every other code reaches the kernel through the value the error carries.
func fuseErrno(err error) int32 {
	var e syscall.Errno
	if errors.As(err, &e) {
		return -int32(e)
	}
	if errors.Is(err, ErrNotExist) {
		return -int32(syscall.ENOENT)
	}
	if errors.Is(err, ErrExist) {
		return -int32(syscall.EEXIST)
	}
	if errors.Is(err, os.ErrPermission) {
		return -int32(syscall.EACCES)
	}
	return -int32(syscall.EIO)
}

func (s *fuseServer) pathOf(nodeid uint64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.nodes[nodeid]
	if !ok {
		return "", syscall.ENOENT
	}
	return p, nil
}

// pathName resolves a node id to its path for the flight window's report. A
// forgotten or never-allocated id renders as "?", so a report never fails on
// a node the server no longer names. The lookup reads the node table under
// its lock, and rendering happens outside the timing aggregate's lock, so the
// two locks never nest. See TheoryOfFlightRecorder.
func (s *fuseServer) pathName(nodeid uint64) string {
	p, err := s.pathOf(nodeid)
	if err != nil {
		return "?"
	}
	return p
}

// afterHeader returns the bytes of a request body that follow a fixed header
// of headerSize bytes. A body shorter than the header has no such bytes: the
// decodeStruct padding makes the fixed fields parse, but a name's start comes
// from the raw length, so the caller reports EINVAL rather than panicking on
// the slice. An unrecovered panic in a handler terminates the process on any
// goroutine, killing the daemon for every later request. See TheoryOfFuseMount.
func afterHeader(body []byte, headerSize int) ([]byte, bool) {
	if headerSize < 0 || len(body) < headerSize {
		return nil, false
	}
	return body[headerSize:], true
}

// splitName splits a NUL-terminated name from the bytes that follow it. ok is
// false when no terminator is present, so a malformed name cannot advance a
// cursor past the body: the reply is EINVAL rather than a panic.
// See TheoryOfFuseMount.
func splitName(b []byte) (name, rest []byte, ok bool) {
	for i, c := range b {
		if c == 0 {
			return b[:i], b[i+1:], true
		}
	}
	return b, nil, false
}

// childPath names one directory entry of a canonical parent without the
// cleaning path.Join performs on every component: a listing builds one path per
// entry, and the parent is already clean and absolute. "." and ".." resolve to
// the parent itself and its own parent, the values path.Join gives, so a listing
// and a later LOOKUP of the same name reach one node id and the kernel sees one
// number for the entry's d_ino and the path's st_ino. The ".." case reads the
// parent's prefix rather than building the same string again. See
// TheoryOfFuseMount.
func childPath(parent, name string) string {
	switch name {
	case ".":
		return parent
	case "..":
		return parentOf(parent)
	}
	if parent == "/" {
		return "/" + name
	}
	return parent + "/" + name
}

// childNodeLocked returns the node id of one directory entry, allocating it on
// first use. The caller holds the server's lock, so a listing resolves every
// entry it carries under one acquisition instead of one per entry: the
// directory walk is the toolchain's dominant pattern, and a listing of a large
// tree repeats that acquisition once per entry. See TheoryOfFuseMount.
func (s *fuseServer) childNodeLocked(parent, name string) uint64 {
	p := childPath(parent, name)
	if id, ok := s.byPath[p]; ok {
		return id
	}
	id := s.nextID
	s.nextID++
	s.nodes[id] = p
	s.byPath[p] = id
	return id
}

// nodeOf returns the node id of p, allocating one on first use. When
// mustExist is true the path is stat'ed first, so an id never names a path the
// store does not hold. The stat runs before the node table's lock is taken:
// it reaches the store's one sqlite connection, and every other user of the
// table — a listing's entry resolution, the flight window's report — would
// wait behind that round trip for the whole lock. A caller that already read
// the path's attribute, the lookup and the create handlers among them, passes
// mustExist false and pays only the map write. See TheoryOfFuseMount.
func (s *fuseServer) nodeOf(p string, mustExist bool) (uint64, error) {
	if mustExist {
		if _, err := s.store.Stat(p); err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byPath[p]; ok {
		return id, nil
	}
	id := s.nextID
	s.nextID++
	s.nodes[id] = p
	s.byPath[p] = id
	return id, nil
}

// fuseUid and fuseGid resolve the mounting process's identity once: an
// attribute render is the most frequent step of a lookup, a getattr, and a
// listing entry, and the identity cannot change during the process's life, so
// two syscalls per rendered attribute buy nothing. A READDIRPLUS reply renders
// one attribute per directory entry, which makes those two syscalls the
// dominant cost of a listing. See TheoryOfAttrCaching.
var (
	fuseUid = sync.OnceValue(os.Getuid)
	fuseGid = sync.OnceValue(os.Getgid)
)

// FUSE open-reply flags, from include/uapi/linux/fuse.h: an open reply asks the
// kernel to keep a file's pages or a directory's entries across opens. Bit 0 of
// the field is FOPEN_DIRECT_IO, which the server never sets: it would bypass the
// page cache and refuse mmap.
// See TheoryOfOpenCaching.
const (
	fuseOpenKeepCache = 1 << 1
	fuseOpenCacheDir  = 1 << 3
)

// attrOfFuse renders one store attribute on the wire. A named pipe reports
// S_IFIFO so the kernel routes pipe opens to the pipe layer, never back to the
// filesystem. A symbolic link reports mode 0o777 whatever its row records,
// because a link's permission bits are not its own and the row may carry the
// bits of a node the link replaced. The process identity arrives from the
// once-resolved values, so a render costs no syscall. See
// TheoryOfCompletePOSIX and TheoryOfAttrCaching.
func attrOfFuse(ino uint64, a Attr) fuseAttr {
	var mode uint32
	switch a.Kind {
	case KindDir:
		mode = syscall.S_IFDIR | uint32(a.Mode.Perm())
	case KindSymlink:
		// Linux reports every symbolic link with mode 0o777: a link's
		// permission bits are not its own, and the store's row may carry the
		// bits of a node the link replaced. See TheoryOfCompletePOSIX.
		mode = syscall.S_IFLNK | 0o777
	case KindFIFO:
		mode = syscall.S_IFIFO | uint32(a.Mode.Perm())
	default:
		mode = syscall.S_IFREG | uint32(a.Mode.Perm())
	}
	nlink := uint32(1)
	if a.Kind == KindDir {
		nlink = 2
	}
	blocks := uint64((a.Size + 511) / 512)
	sec := a.ModTime.Unix()
	nsec := uint32(a.ModTime.Nanosecond())
	return fuseAttr{
		Ino: ino, Size: uint64(a.Size), Blocks: blocks,
		Atime: uint64(sec), Atimensec: nsec,
		Mtime: uint64(sec), Mtimensec: nsec,
		Ctime: uint64(sec), Ctimensec: nsec,
		Mode: mode, Nlink: nlink,
		Uid: uint32(fuseUid()), Gid: uint32(fuseGid()),
		Blksize: 4096,
	}
}

func (s *fuseServer) entryOut(id uint64, a Attr) fuseEntryOut {
	return fuseEntryOut{
		Nodeid:     id,
		Generation: 1,
		EntryValid: fuseCacheTimeoutSec,
		AttrValid:  fuseCacheTimeoutSec,
		Attr:       attrOfFuse(id, a),
	}
}

// handleInit answers the kernel's INIT with the capability set the server
// supports and the request ceiling those capabilities buy: BIG_WRITES lifts
// the one-page write limit, and MAX_PAGES raises the announced window to
// fuseMaxWriteMax. Only bits the kernel offers are echoed, so an older
// kernel keeps the conservative window. The handshake also fixes the protocol
// version: a newer kernel is told to fall back to fuseProtocolMajor, and an
// older one fails with EPROTO. The announced queue depth keeps the readahead
// the echoed window promises: every readahead read is a background request,
// and a shallow queue would throttle them before the server is ever busy.
// See TheoryOfInitNegotiation.
func (s *fuseServer) handleInit(body []byte) ([]byte, error) {
	var in fuseInitIn
	decodeStruct(&in, body)
	// The kernel names its protocol version here. This server speaks major 7,
	// so a newer kernel is told to fall back to 7; the kernel then reopens the
	// handshake at that version. An older kernel speaks a protocol this server
	// cannot serve, so the negotiation fails with EPROTO. See
	// TheoryOfInitNegotiation.
	if in.Major < fuseProtocolMajor {
		return nil, syscall.EPROTO
	}
	minor := in.Minor
	if minor > fuseProtocolMinor {
		minor = fuseProtocolMinor
	}
	flags := in.Flags & fuseInitWanted
	out := fuseInitOut{
		Major:               fuseProtocolMajor,
		Minor:               minor,
		MaxReadahead:        in.MaxReadahead,
		Flags:               flags,
		MaxBackground:       fuseMaxBackground,
		CongestionThreshold: fuseCongestionThreshold,
		MaxWrite:            fuseMaxWrite,
		TimeGran:            1,
	}
	if flags&fuseInitMaxPages != 0 {
		out.MaxWrite = fuseMaxWriteMax
		out.MaxPages = fuseMaxPages
	}
	return encodeStruct(&out), nil
}

func (s *fuseServer) handleLookup(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	// A body that carries no name at all names nothing, so the answer is
	// ENOENT; a body whose name runs to the end without a terminator is
	// malformed and reports EINVAL. See TheoryOfFuseMount.
	if len(body) == 0 {
		return nil, syscall.ENOENT
	}
	name, ok := nameOf(body)
	if !ok {
		return nil, syscall.EINVAL
	}
	// A name that carries a separator cannot name one entry of the parent:
	// joining it would walk out of that parent. A name of "." or ".." names no
	// entry at all. See TheoryOfFuseMount.
	if !validEntryName(name) {
		if name == "" || name == "." || name == ".." {
			return nil, syscall.ENOENT
		}
		return nil, syscall.EINVAL
	}
	// childPath builds the entry's path without the cleaning path.Join
	// performs, and it is the same join a listing uses, so a LOOKUP and a
	// listing reach one node id for one name. See TheoryOfFuseMount.
	p := childPath(parentPath, name)
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	id, err := s.nodeOf(p, false)
	if err != nil {
		return nil, err
	}
	// The entry body is built once per lookup, so it is a local value: a slice
	// literal whose element the encoder addresses would allocate beside the
	// reply the kernel parses anyway. See TheoryOfFuseMount.
	entry := s.entryOut(id, a)
	return encodeStruct(&entry), nil
}

func (s *fuseServer) handleGetattr(nodeid uint64) ([]byte, error) {
	p, err := s.pathOf(nodeid)
	if err != nil {
		return nil, err
	}
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	return encodeStruct(&fuseAttrOut{
		AttrValid: fuseCacheTimeoutSec,
		Attr:      attrOfFuse(nodeid, a),
	}), nil
}

func (s *fuseServer) handleSetattr(nodeid uint64, body []byte) ([]byte, error) {
	p, err := s.pathOf(nodeid)
	if err != nil {
		return nil, err
	}
	var in fuseSetattrIn
	decodeStruct(&in, body)
	cur, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	if in.Valid&fattrSize != 0 {
		// The mount and the store must agree on the bound: a truncate the
		// mount accepts is a size the store can hold, so both read the one
		// constant the store enforces. See TheoryOfCompletePOSIX.
		if in.Size > maxFileSize {
			return nil, syscall.EFBIG
		}
		// The file's own mode travels beside the length: a materialised base
		// file keeps the bits it had, and a recorded 000 is a mode, not an
		// absent one. Truncation touches the chunks beyond the new length and
		// the chunk the length ends inside; no content passes through this
		// handler. See TheoryOfChunkedContent.
		if err := s.store.TruncateFile(p, int64(in.Size), cur.Mode); err != nil {
			return nil, err
		}
		s.invalidateHandles(p)
	}
	if in.Valid&fattrMode != 0 {
		if err := s.store.SetMode(p, os.FileMode(in.Mode&0o777)); err != nil {
			return nil, err
		}
	}
	if in.Valid&(fattrMtime|fattrMtimeNow) != 0 {
		mt := time.Unix(int64(in.Mtime), int64(in.Mtimensec))
		if in.Valid&fattrMtimeNow != 0 {
			mt = time.Now()
		}
		if err := s.store.SetTimes(p, mt); err != nil {
			return nil, err
		}
	}
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	return encodeStruct(&fuseAttrOut{
		AttrValid: fuseCacheTimeoutSec,
		Attr:      attrOfFuse(nodeid, a),
	}), nil
}

// invalidateHandles marks every open handle of p stale, so a read that follows
// a truncation serves the store's new content instead of a descriptor that
// fixes the old view. The store is the only copy of the content, so no other
// state needs dropping. A self of 0 names no handle, so every handle of the
// path is marked. See TheoryOfReadPath and TheoryOfFuseConcurrency.
func (s *fuseServer) invalidateHandles(p string) {
	s.invalidateOtherHandles(p, 0)
}

// invalidateOtherHandles marks the cached base reader of every open handle of p
// except the writing one stale, so their next read serves the store's
// now-current content. Handles hold no buffer, so a write through one handle
// cannot be overwritten by another handle's stale bytes; only the base view
// must go. The mark is an atomic flag, never a wait on the other handle's
// lock: this call runs under the writer's own lock, and a writer that blocked
// on another handle would deadlock with the second writer blocking on the
// first. The node table's lock is released before the flags are set, so no
// lock is held across the mark. See TheoryOfWriteSplice and
// TheoryOfFuseConcurrency.
func (s *fuseServer) invalidateOtherHandles(p string, self uint64) {
	s.mu.Lock()
	held := make([]*fuseHandle, 0, 2)
	for fh, h := range s.handles {
		if fh == self || h.path != p {
			continue
		}
		held = append(held, h)
	}
	s.mu.Unlock()
	for _, h := range held {
		h.baseStale.Store(true)
	}
}

// invalidateDirListings marks the open listings of a directory stale, so a read
// that starts over reseeds them from the store. The entries already delivered
// and the cursor's position stay: resetting them would answer the kernel's next
// cookie with an empty body, which the kernel reads as the end of the
// directory, and a walk that a create or a delete interrupted would lose every
// entry past the mutation. The kernel cannot see a change this mount makes on
// its own, and FOPEN_CACHE_DIR keeps the listing it read, so the stale mark is
// the server's own notice. The matching handles are collected under the node
// table's lock and marked under their own, one at a time: a listing request may
// be using its handle at this moment, and holding the node table across that
// wait would nest the two locks in the order a listing takes them the other way
// round. See TheoryOfOpenCaching, TheoryOfReadPath, TheoryOfDirectoryStream
// and TheoryOfFuseConcurrency.
func (s *fuseServer) invalidateDirListings(p string) {
	s.mu.Lock()
	held := make([]*fuseHandle, 0, 2)
	for _, h := range s.handles {
		if h.isDir && h.path == p {
			held = append(held, h)
		}
	}
	s.mu.Unlock()
	for _, h := range held {
		h.mu.Lock()
		h.dirStale = true
		h.mu.Unlock()
	}
}

// seedListing opens a fresh cursor for one directory handle and fills the
// entries that lead its reply: "." reports the directory's own attribute and
// ".." its parent's, both from the reads openDir already paid, so a listing
// costs no second stat of the directory it just checked.
// See TheoryOfReadPath and TheoryOfDirectoryStream.
func (s *fuseServer) seedListing(h *fuseHandle) error {
	h.closeDir()
	h.entries = h.entries[:0]
	h.dirWindow = h.dirWindow[:0]
	dir, err := s.store.openDir(h.path)
	if err != nil {
		return err
	}
	h.dir = dir
	h.entries = append(h.entries, DirEntry{Name: ".", Attr: dir.self})
	if dir.hasParent {
		h.entries = append(h.entries, DirEntry{Name: "..", Attr: dir.parent})
	}
	return nil
}

// refreshListing reseeds a listing a mutation through this mount marked stale,
// but only for a read from the first entry. A continued read keeps the stream
// it started with and leaves the mark in place, so a later rewind still reseeds:
// the entries the kernel holds cookies for stay addressable, and positioning a
// fresh cursor at an arbitrary index would walk every entry before it in one
// request — the cost this design removes — while a walk that shifted its own
// offsets mid-way would repeat or skip a name. See TheoryOfDirectoryStream.
func (s *fuseServer) refreshListing(h *fuseHandle, off int) error {
	if !h.dirStale || off != 0 {
		return nil
	}
	h.dirStale = false
	return s.seedListing(h)
}

// dirEntryAt returns the entry at one index of a handle's listing, pulling one
// window from the cursor when the index lies past what the handle holds. ok is
// false at the end of the listing, and the cursor is then released.
// See TheoryOfDirectoryStream.
func (s *fuseServer) dirEntryAt(h *fuseHandle, i int) (entry DirEntry, ok bool, err error) {
	for i >= len(h.entries) {
		if h.dir == nil {
			return DirEntry{}, false, nil
		}
		more, err := h.dir.next(dirBatchEntries)
		if err != nil {
			return DirEntry{}, false, err
		}
		if len(more) == 0 {
			h.closeDir()
			return DirEntry{}, false, nil
		}
		h.entries = append(h.entries, more...)
	}
	return h.entries[i], true, nil
}

// direntRecordSize is the padded length one READDIR record occupies: the fixed
// dirent, the name, and the padding that aligns the next record.
func direntRecordSize(name string) int {
	return (fuseDirentSize + len(name) + 7) &^ 7
}

// entryRecordSize is the padded length one READDIRPLUS record occupies: the
// entry body, the dirent, the name, and the padding.
func entryRecordSize(name string) int {
	return (fuseEntryOutSize + fuseDirentSize + len(name) + 7) &^ 7
}

func (s *fuseServer) handleOpen(nodeid uint64, body []byte) ([]byte, error) {
	var in fuseOpenIn
	decodeStruct(&in, body)
	p, err := s.pathOf(nodeid)
	if err != nil {
		return nil, err
	}
	writeMode := in.Flags&(uint32(unix.O_WRONLY)|uint32(unix.O_RDWR)) != 0
	// O_TRUNC truncates whatever the access mode: POSIX gives the flag the same
	// effect on a read-only open as on a write-mode one. The truncate is a
	// mutation, so it passes the write policy even when the open itself is
	// read-only, and a refused path sees EACCES at open rather than a file
	// emptied behind the policy's back. See TheoryOfCompletePOSIX and
	// TheoryOfWritePolicy.
	truncate := in.Flags&uint32(unix.O_TRUNC) != 0
	if writeMode || truncate {
		if err := s.store.CanWrite(p); err != nil {
			return nil, err
		}
	}
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	if a.Kind == KindDir {
		return nil, syscall.EISDIR
	}
	if truncate && a.Kind == KindRegular {
		// The file's own mode travels beside the length: a materialised base
		// file keeps the bits it had, and a recorded 000 is a mode, not an
		// absent one. A truncating open drops the content chunks; it never
		// writes the file back. See TheoryOfChunkedContent.
		if err := s.store.TruncateFile(p, 0, a.Mode); err != nil {
			return nil, err
		}
		s.invalidateHandles(p)
	}
	// The open already read the attribute, so the handle carries the mode and
	// the write path needs no lookup of its own. The store keeps the content
	// in its chunk table, so a truncating open leaves nothing for the handle
	// to remember. See TheoryOfWriteSplice.
	fh := s.registerHandle(&fuseHandle{path: p, mode: a.Mode})
	// A read-only open lets the kernel keep the file's pages across opens, so
	// a repeated read of an unchanged dependency never reaches the store. A
	// write-mode or truncating open must not: its content is the session's to
	// change, and a page kept from before would shadow the bytes the request
	// just stored. See TheoryOfOpenCaching.
	out := fuseOpenOut{Fh: fh}
	if !writeMode && !truncate {
		out.OpenFlags = fuseOpenKeepCache
	}
	return encodeStruct(&out), nil
}

func (s *fuseServer) handleRead(body []byte) ([]byte, error) {
	var in fuseReadIn
	decodeStruct(&in, body)
	s.mu.Lock()
	h := s.handles[in.Fh]
	s.mu.Unlock()
	if h == nil {
		return nil, syscall.EBADF
	}
	if h.isDir {
		return nil, syscall.EISDIR
	}
	if in.Size == 0 {
		return []byte{}, nil
	}
	return s.readFrom(h, int64(in.Offset), int(in.Size))
}

func (s *fuseServer) baseHandleFile(h *fuseHandle) (*os.File, error) {
	// A write or a truncation of the path, possibly through another handle,
	// marked the cached reader stale. The mark is consumed here, under this
	// handle's own lock, so a descriptor that fixes a view the store has since
	// replaced never answers a window. See TheoryOfFuseConcurrency and
	// TheoryOfReadPath.
	if h.baseStale.Swap(false) {
		h.closeBase()
	}
	if h.base != nil {
		return h.base, nil
	}
	rel := relPath(h.path)
	f, fast, err := baseOpenFile(s.store.baseFd, rel)
	if !fast {
		// The platform offers no base descriptor, so the kind check and the
		// open run through the portable walk. The check must precede the open:
		// an open of a pipe blocks one worker of the request pool until a peer
		// appears, and four such waits stop the whole mount, while an open of
		// a link would serve its target's bytes. See TheoryOfBaseFdAccess.
		a, statErr := s.store.Stat(h.path)
		if statErr != nil {
			return nil, statErr
		}
		switch a.Kind {
		case KindDir:
			return nil, syscall.EISDIR
		case KindSymlink, KindFIFO:
			return nil, syscall.EINVAL
		}
		// The attribute above may come from an upper row, which records what
		// the session made of the path rather than what the base holds. The
		// base node is therefore read before the open: a device and a socket
		// are not content, and opening one waits for its peer. See
		// TheoryOfBaseCapture.
		info, lerr := s.store.baseRoot.Lstat(rel)
		if lerr != nil {
			return nil, lerr
		}
		if !info.Mode().IsRegular() {
			return nil, syscall.EINVAL
		}
		f, err = s.store.baseRoot.Open(rel)
	}
	if err != nil {
		return nil, err
	}
	h.base = f
	return f, nil
}

func (s *fuseServer) readFrom(h *fuseHandle, off int64, size int) ([]byte, error) {
	if off < 0 || size < 0 {
		return nil, syscall.EINVAL
	}
	// A size above the store's bound is a claim no kernel makes: it sends a
	// read in windows no larger than the announced request ceiling, so a larger
	// value is malformed and must not become an allocation. The bound also
	// keeps the int conversion of a huge 32-bit size from reaching the window
	// buffer. See TheoryOfCompletePOSIX.
	if size > maxFileSize {
		return nil, syscall.EFBIG
	}
	chunk, kind, found, err := s.store.readUpperRange(h.path, off, size)
	if err != nil {
		return nil, err
	}
	if found {
		switch kind {
		case KindDir:
			return nil, syscall.EISDIR
		case KindSymlink, KindFIFO:
			return nil, syscall.EINVAL
		}
		s.store.auditRead(h.path)
		return chunk, nil
	}
	f, err := s.baseHandleFile(h)
	if err != nil {
		return nil, err
	}
	// The window reuses the handle's buffer, so a streamed file costs one
	// allocation per handle rather than one per window.
	// See TheoryOfReadPath.
	if cap(h.readBuf) < size {
		h.readBuf = make([]byte, size)
	}
	buf := h.readBuf[:size]
	n := 0
	for {
		n, err = unix.Pread(int(f.Fd()), buf, off)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	s.store.auditRead(h.path)
	return buf[:n], nil
}

func (s *fuseServer) handleWrite(body []byte) ([]byte, error) {
	var in fuseWriteIn
	decodeStruct(&in, body)
	// A zero-byte write is a no-op, matching Linux: write(2) with nbyte=0
	// transfers nothing and does not extend the file, even when the offset
	// sits past the end. See TheoryOfCompletePOSIX.
	if in.Size == 0 {
		return encodeStruct(&fuseWriteOut{Size: 0}), nil
	}
	// The offset, the size, and their sum are bounded before the body length
	// is computed, so a malformed claim never slices a body or reserves memory
	// for bytes that cannot exist: on a 32-bit build a size above 2^31 turns
	// into a negative int, and a length check that ran first would pass on a
	// body the slice below cannot hold. Each operand of the sum is bounded, so
	// the sum itself cannot wrap. See TheoryOfCompletePOSIX.
	if in.Offset > uint64(maxFileSize) ||
		uint64(in.Size) > uint64(maxFileSize) ||
		in.Offset+uint64(in.Size) > uint64(maxFileSize) {
		return nil, syscall.EFBIG
	}
	hdrSize := binary.Size(in)
	if len(body) < hdrSize+int(in.Size) {
		return nil, syscall.EINVAL
	}
	data := body[hdrSize : hdrSize+int(in.Size)]
	s.mu.Lock()
	h := s.handles[in.Fh]
	s.mu.Unlock()
	if h == nil {
		return nil, syscall.EBADF
	}
	// The mode comes from the open that handed the handle back, so the write
	// path pays no metadata lookup, and it travels as given, 000 included: a
	// materialised file with no access bits is a choice, not an absent mode.
	// The delta goes straight into the stored chunks, so the store copies the
	// payload rather than the file and the handle holds no copy at all.
	// See TheoryOfWriteSplice and TheoryOfCompletePOSIX.
	if err := s.store.SpliceFile(h.path, int64(in.Offset), data, h.mode); err != nil {
		return nil, err
	}
	// The store now holds this write, so the handle's cached base reader can
	// never serve another window.
	h.closeBase()
	// A write through one handle is now the current store content; other
	// handles over the same path must drop their cached base view so their
	// next read serves it. See invalidateOtherHandles.
	s.invalidateOtherHandles(h.path, in.Fh)
	return encodeStruct(&fuseWriteOut{Size: in.Size}), nil
}

func (s *fuseServer) handleRelease(body []byte) ([]byte, error) {
	var in fuseReleaseIn
	decodeStruct(&in, body)
	s.mu.Lock()
	h := s.handles[in.Fh]
	delete(s.handles, in.Fh)
	s.mu.Unlock()
	if h != nil {
		h.closeBase()
		h.closeDir()
	}
	return nil, nil
}

func (s *fuseServer) handleOpendir(nodeid uint64) ([]byte, error) {
	p, err := s.pathOf(nodeid)
	if err != nil {
		return nil, err
	}
	// Opening a listing reads the directory's own attributes, the upper layer's
	// rows for it, and one handle on the base directory. It enumerates nothing:
	// a request that walked the directory would make its duration grow with the
	// directory's entry count, and a request that runs long holds one worker of
	// the request pool for its whole duration. See TheoryOfDirectoryStream and
	// TheoryOfFuseConcurrency.
	h := &fuseHandle{path: p, isDir: true}
	if err := s.seedListing(h); err != nil {
		h.closeDir()
		return nil, err
	}
	fh := s.registerHandle(h)
	// The kernel keeps the listing across opens, so a repeated walk costs no
	// request. A mutation through this mount marks the listing stale, and the
	// next read from the first entry reseeds it from the store, while a
	// continued read keeps the stream it started with.
	// See TheoryOfOpenCaching, TheoryOfReadPath and TheoryOfDirectoryStream.
	return encodeStruct(&fuseOpenOut{Fh: fh, OpenFlags: fuseOpenCacheDir}), nil
}

func (s *fuseServer) handleReaddir(body []byte) ([]byte, error) {
	var in fuseReadIn
	decodeStruct(&in, body)
	s.mu.Lock()
	h := s.handles[in.Fh]
	s.mu.Unlock()
	if h == nil || !h.isDir {
		return nil, syscall.ENOTDIR
	}
	start := int(in.Offset)
	// A cookie past the entries the handle already holds names a reply the
	// kernel never received, so the answer is the empty body a read at the end
	// of a listing gets: pulling the listing forward to it would walk entries
	// no reply carries. See TheoryOfDirectoryStream.
	if start < 0 || start > len(h.entries) {
		return []byte{}, nil
	}
	if err := s.refreshListing(h, start); err != nil {
		return nil, err
	}
	maxSize := int(in.Size)
	// The entries that fit are gathered before the node table's lock: one past
	// the handle's window comes from the cursor, which reads the base and the
	// store, and the lock must not cover that work. The gather also stops once
	// this reply has pulled one window from the cursor, although the announced
	// window could hold far more: a request's duration must not grow with the
	// directory's entry count, and the kernel asks again at the cookie for the
	// rest. See TheoryOfDirectoryStream.
	h.dirWindow = h.dirWindow[:0]
	held := len(h.entries)
	used := 0
	for i := start; ; i++ {
		e, ok, err := s.dirEntryAt(h, i)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		padded := direntRecordSize(e.Name)
		if used+padded > maxSize {
			break
		}
		h.dirWindow = append(h.dirWindow, e)
		used += padded
		if len(h.entries)-held >= dirBatchEntries {
			break
		}
	}
	// The scratch is sized from the entries that fit rather than the announced
	// window, which is the negotiated megabyte even for a small directory.
	// See TheoryOfInitNegotiation.
	if cap(h.listBuf) < used {
		h.listBuf = make([]byte, 0, used)
	}
	out := h.listBuf[:0]
	// One acquisition of the node table serves the whole reply, and each
	// entry's path comes from appending to the canonical parent. The dirent's
	// inode number is the node id the kernel gets from a LOOKUP of the same
	// name: a tool that compares a listing's d_ino with a stat's st_ino must
	// see one number, not two. See TheoryOfFuseMount.
	s.mu.Lock()
	for k, e := range h.dirWindow {
		padded := direntRecordSize(e.Name)
		id := s.childNodeLocked(h.path, e.Name)
		prev := len(out)
		out = out[:prev+padded]
		// The scratch is reused, so the name's trailing padding would otherwise
		// carry the previous reply's bytes.
		clear(out[prev:])
		binary.LittleEndian.PutUint64(out[prev:], id)
		binary.LittleEndian.PutUint64(out[prev+8:], uint64(start+k+1))
		binary.LittleEndian.PutUint32(out[prev+16:], uint32(len(e.Name)))
		binary.LittleEndian.PutUint32(out[prev+20:], direntType(e.Attr.Kind))
		copy(out[prev+fuseDirentSize:], e.Name)
	}
	s.mu.Unlock()
	h.listBuf = out
	return out, nil
}

func (s *fuseServer) handleReaddirPlus(body []byte) ([]byte, error) {
	var in fuseReadIn
	decodeStruct(&in, body)
	s.mu.Lock()
	h := s.handles[in.Fh]
	s.mu.Unlock()
	if h == nil || !h.isDir {
		return nil, syscall.ENOTDIR
	}
	start := int(in.Offset)
	// A cookie past the entries the handle already holds names a reply the
	// kernel never received; see handleReaddir.
	// See TheoryOfDirectoryStream.
	if start < 0 || start > len(h.entries) {
		return []byte{}, nil
	}
	if err := s.refreshListing(h, start); err != nil {
		return nil, err
	}
	maxSize := int(in.Size)
	// The entries that fit are gathered before the node table's lock, and the
	// gather stops once this reply has pulled one window from the cursor, the
	// same split handleReaddir makes. See TheoryOfDirectoryStream.
	h.dirWindow = h.dirWindow[:0]
	held := len(h.entries)
	used := 0
	for i := start; ; i++ {
		e, ok, err := s.dirEntryAt(h, i)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		padded := entryRecordSize(e.Name)
		if used+padded > maxSize {
			break
		}
		h.dirWindow = append(h.dirWindow, e)
		used += padded
		if len(h.entries)-held >= dirBatchEntries {
			break
		}
	}
	if cap(h.listBuf) < used {
		h.listBuf = make([]byte, 0, used)
	}
	out := h.listBuf[:0]
	// One acquisition of the node table serves the whole reply. The dirent
	// carries the same node id the entry_out ahead of it announces: one record
	// must not name two inodes. See TheoryOfFuseMount.
	s.mu.Lock()
	for k, e := range h.dirWindow {
		padded := entryRecordSize(e.Name)
		id := s.childNodeLocked(h.path, e.Name)
		prev := len(out)
		out = out[:prev+padded]
		// The scratch is reused, so the name's trailing padding would otherwise
		// carry the previous reply's bytes.
		clear(out[prev:])
		entry := s.entryOut(id, e.Attr)
		putEntryOut(out[prev:], &entry)
		binary.LittleEndian.PutUint64(out[prev+fuseEntryOutSize:], id)
		binary.LittleEndian.PutUint64(out[prev+fuseEntryOutSize+8:], uint64(start+k+1))
		binary.LittleEndian.PutUint32(out[prev+fuseEntryOutSize+16:], uint32(len(e.Name)))
		binary.LittleEndian.PutUint32(out[prev+fuseEntryOutSize+20:], direntType(e.Attr.Kind))
		copy(out[prev+fuseEntryOutSize+fuseDirentSize:], e.Name)
	}
	s.mu.Unlock()
	h.listBuf = out
	return out, nil
}

// handleAccess answers FUSE_ACCESS. The reply carries no body; only the error
// code speaks. The path's existence comes first, so a probe of a path the
// session deleted reports ENOENT, and a W_OK probe asks the write policy, so a
// caller learns at access time what its first write would report.
// See TheoryOfWritePolicy.
func (s *fuseServer) handleAccess(nodeid uint64, body []byte) ([]byte, error) {
	p, err := s.pathOf(nodeid)
	if err != nil {
		return nil, err
	}
	var in fuseAccessIn
	decodeStruct(&in, body)
	if _, err := s.store.Stat(p); err != nil {
		return nil, err
	}
	if in.Mask&unix.W_OK != 0 {
		if err := s.store.CanWrite(p); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func direntType(k Kind) uint32 {
	switch k {
	case KindDir:
		return uint32(syscall.S_IFDIR >> 12)
	case KindSymlink:
		return uint32(syscall.S_IFLNK >> 12)
	case KindFIFO:
		return uint32(syscall.S_IFIFO >> 12)
	default:
		return uint32(syscall.S_IFREG >> 12)
	}
}

// registerHandle files one handle under a fresh handle number. Every handler
// that hands a handle to the kernel runs it, so no handler carries its own copy
// of the counter and the map write.
func (s *fuseServer) registerHandle(h *fuseHandle) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	fh := s.nextFh
	s.nextFh++
	s.handles[fh] = h
	return fh
}

func (s *fuseServer) handleCreate(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	var in fuseCreateIn
	decodeStruct(&in, body)
	rest, ok := afterHeader(body, binary.Size(in))
	if !ok {
		return nil, syscall.EINVAL
	}
	name, ok := entryName(rest)
	if !ok {
		return nil, syscall.EINVAL
	}
	p := path.Join(parentPath, name)
	// The mode the create request carries is masked by the umask it also
	// carries: the kernel hands the filesystem both, and a filesystem that
	// ignores the mask hands the caller bits the process's umask withdrew. A
	// mode of no bits stays a choice below the mask.
	// See TheoryOfCompletePOSIX.
	perm := os.FileMode(in.Mode&0o777) &^ os.FileMode(in.Umask&0o777)
	cur, statErr := s.store.Stat(p)
	created := false
	switch {
	case statErr == nil:
		if in.Flags&uint32(unix.O_EXCL) != 0 {
			return nil, ErrExist
		}
		if cur.Kind == KindDir {
			return nil, syscall.EISDIR
		}
		if in.Flags&uint32(unix.O_TRUNC) != 0 {
			// The create's mode describes a file that does not exist yet; an
			// existing file keeps the mode it already has — a recorded 000 is
			// a mode, not an absent one — and the create changes nothing but
			// the length. See TheoryOfCompletePOSIX.
			if err := s.store.TruncateFile(p, 0, cur.Mode); err != nil {
				return nil, err
			}
			// A truncation drops every cached base reader of the path, the
			// same way the truncating open does: a handle that opened the
			// file earlier must serve the store's new content, not the
			// descriptor that fixes the old view. See TheoryOfReadPath.
			s.invalidateHandles(p)
		} else if in.Flags&(uint32(unix.O_WRONLY)|uint32(unix.O_RDWR)) != 0 {
			// A create-open that finds the path already there and carries no
			// O_TRUNC mutates nothing by itself, so only a write-mode request
			// asks the policy — the same rule a plain OPEN follows. A
			// read-only create-open therefore reaches an existing file the
			// sandbox may not write. See TheoryOfWritePolicy and
			// TheoryOfCompletePOSIX.
			if err := s.store.CanWrite(p); err != nil {
				return nil, err
			}
		}
	case errors.Is(statErr, ErrNotExist):
		if err := s.store.WriteFile(p, nil, perm); err != nil {
			return nil, err
		}
		created = true
	default:
		return nil, statErr
	}
	if created {
		s.invalidateDirListings(parentPath)
	}
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	// The create above established the row and the stat above read its
	// attribute, so the node id needs no existence check of its own: a third
	// stat would pay a store round trip the handler already made.
	id, err := s.nodeOf(p, false)
	if err != nil {
		return nil, err
	}
	// The stat above already read the attribute, so the handle carries the
	// mode and the write path needs no lookup of its own. The store keeps the
	// content in its chunk table, so nothing about the create state belongs to
	// the handle. See TheoryOfWriteSplice.
	fh := s.registerHandle(&fuseHandle{path: p, mode: a.Mode})
	return encodeStruct(&fuseCreateOut{
		Entry: s.entryOut(id, a),
		Open:  fuseOpenOut{Fh: fh},
	}), nil
}

func (s *fuseServer) handleMkdir(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	var in fuseMkdirIn
	decodeStruct(&in, body)
	rest, ok := afterHeader(body, binary.Size(in))
	if !ok {
		return nil, syscall.EINVAL
	}
	name, ok := entryName(rest)
	if !ok {
		return nil, syscall.EINVAL
	}
	p := path.Join(parentPath, name)
	// The mode is masked by the request's umask, the same mask a local
	// filesystem applies, and a mode of no bits stays a choice below it.
	// See TheoryOfCompletePOSIX.
	perm := os.FileMode(in.Mode&0o777) &^ os.FileMode(in.Umask&0o777)
	if err := s.store.Mkdir(p, perm); err != nil {
		return nil, err
	}
	s.invalidateDirListings(parentPath)
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	id, err := s.nodeOf(p, true)
	if err != nil {
		return nil, err
	}
	entry := s.entryOut(id, a)
	return encodeStruct(&entry), nil
}

func (s *fuseServer) handleMknod(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	var in fuseMknodIn
	decodeStruct(&in, body)
	rest, ok := afterHeader(body, binary.Size(in))
	if !ok {
		return nil, syscall.EINVAL
	}
	name, ok := entryName(rest)
	if !ok {
		return nil, syscall.EINVAL
	}
	if in.Mode&uint32(syscall.S_IFMT) != uint32(syscall.S_IFIFO) {
		return nil, syscall.EPERM
	}
	p := path.Join(parentPath, name)
	// The mode is masked by the request's umask, the same mask a local
	// filesystem applies, and a mode of no bits stays a choice below it.
	// See TheoryOfCompletePOSIX.
	perm := os.FileMode(in.Mode&0o777) &^ os.FileMode(in.Umask&0o777)
	if err := s.store.Mkfifo(p, perm); err != nil {
		return nil, err
	}
	s.invalidateDirListings(parentPath)
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	id, err := s.nodeOf(p, true)
	if err != nil {
		return nil, err
	}
	entry := s.entryOut(id, a)
	return encodeStruct(&entry), nil
}

// handleUnlink serves UNLINK and RMDIR with POSIX type checks: UNLINK of a
// directory is EISDIR and RMDIR of a regular file is ENOTDIR, so a caller's
// unlink-or-rmdir fallback sees the same errors a local filesystem reports.
// A name with no terminator, or one that cannot name a single entry, reports
// EINVAL, and a successful removal clears the parent's cached listings.
// See TheoryOfCompletePOSIX and TheoryOfFuseMount.
func (s *fuseServer) handleUnlink(parent uint64, body []byte, isDir bool) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	name, ok := entryName(body)
	if !ok {
		return nil, syscall.EINVAL
	}
	p := path.Join(parentPath, name)
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	switch {
	case isDir && a.Kind != KindDir:
		return nil, syscall.ENOTDIR
	case !isDir && a.Kind == KindDir:
		return nil, syscall.EISDIR
	}
	if err := s.store.Remove(p); err != nil {
		return nil, err
	}
	s.invalidateDirListings(parentPath)
	return nil, nil
}

func (s *fuseServer) handleRename(parent uint64, body []byte, two bool) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	var newdir uint64
	var flags uint32
	var hdrSize int
	if two {
		var in fuseRename2In
		decodeStruct(&in, body)
		newdir = in.Newdir
		flags = in.Flags
		hdrSize = binary.Size(in)
	} else {
		var in fuseRenameIn
		decodeStruct(&in, body)
		newdir = in.Newdir
		hdrSize = binary.Size(in)
	}
	// The fixed header is checked before the node id it carries is resolved:
	// a short body decodes the id as zero, and a lookup of that id would
	// report ENOENT for a malformed request where EINVAL is the answer.
	// See TheoryOfFuseMount.
	rest, ok := afterHeader(body, hdrSize)
	if !ok {
		return nil, syscall.EINVAL
	}
	newParent, err := s.pathOf(newdir)
	if err != nil {
		return nil, err
	}
	oldNameB, rest, ok := splitName(rest)
	if !ok || !validEntryName(string(oldNameB)) {
		return nil, syscall.EINVAL
	}
	// The destination name needs its terminator and the same single-entry
	// rule as the source: a name that runs to the end of the body cannot be
	// told from one the sender truncated, and a name with a separator would
	// walk out of the destination parent.
	newName, ok := entryName(rest)
	if !ok {
		return nil, syscall.EINVAL
	}
	oldPath := path.Join(parentPath, string(oldNameB))
	newPath := path.Join(newParent, newName)
	const (
		renameNoReplace = 1
		renameExchange  = 2
		renameWhiteout  = 4
	)
	if flags&renameExchange != 0 || flags&renameWhiteout != 0 {
		return nil, syscall.ENOSYS
	}
	if flags&renameNoReplace != 0 {
		// The source lookup runs first, matching renameat2: a missing
		// source reports ENOENT even when the destination exists.
		if _, err := s.store.Stat(oldPath); err != nil {
			return nil, err
		}
		if _, err := s.store.Stat(newPath); err == nil {
			return nil, ErrExist
		}
	}
	if err := s.store.Rename(oldPath, newPath); err != nil {
		return nil, err
	}
	// Both directories lost and gained an entry, so both listings rebuild.
	// A rename inside one directory asks the same path twice, which the loop
	// walks without harm. See TheoryOfReadPath.
	s.invalidateDirListings(parentPath)
	s.invalidateDirListings(newParent)
	return nil, nil
}

// handleSymlink creates a symbolic link. The kernel wire form is
// link_name\0link_target\0 — the name first, matching FUSE_SYMLINK's
// documented fields — so the two strings decode in that order. The name must
// name a single entry, while the target may hold any string: a link target is
// not a name and is resolved by the kernel later. A body whose name carries no
// terminator cannot name a target, and a target with no terminator is
// malformed too, so both report EINVAL. A terminated but empty target is not
// malformed: it names no target, which POSIX refuses with ENOENT through the
// store. See TheoryOfCompletePOSIX and TheoryOfFuseMount.
func (s *fuseServer) handleSymlink(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	nameB, rest, ok := splitName(body)
	if !ok {
		return nil, syscall.EINVAL
	}
	name := string(nameB)
	if !validEntryName(name) {
		return nil, syscall.EINVAL
	}
	// An empty target passes through: the store answers the ENOENT that
	// symlink(2) reports, and the wire layer keeps only the malformed body
	// for itself.
	target, ok := nameOf(rest)
	if !ok {
		return nil, syscall.EINVAL
	}
	p := path.Join(parentPath, name)
	if err := s.store.Symlink(target, p); err != nil {
		return nil, err
	}
	s.invalidateDirListings(parentPath)
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	id, err := s.nodeOf(p, true)
	if err != nil {
		return nil, err
	}
	// The entry body is built once per create, so it is a local value: a slice
	// literal whose element the encoder addresses would allocate beside the
	// reply the kernel parses anyway, the same shape handleLookup removed.
	// See TheoryOfFuseMount.
	entry := s.entryOut(id, a)
	return encodeStruct(&entry), nil
}

func (s *fuseServer) handleReadlink(nodeid uint64) ([]byte, error) {
	p, err := s.pathOf(nodeid)
	if err != nil {
		return nil, err
	}
	target, err := s.store.Readlink(p)
	if err != nil {
		return nil, err
	}
	return []byte(target), nil
}

func (s *fuseServer) handleLink(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	var in fuseLinkIn
	decodeStruct(&in, body)
	// The fixed header is checked before the node id it carries is resolved:
	// a short body decodes the id as zero, and a lookup of that id would
	// report ENOENT for a malformed request where EINVAL is the answer.
	// See TheoryOfFuseMount.
	rest, ok := afterHeader(body, binary.Size(in))
	if !ok {
		return nil, syscall.EINVAL
	}
	oldPath, err := s.pathOf(in.Oldnodeid)
	if err != nil {
		return nil, err
	}
	name, ok := entryName(rest)
	if !ok {
		return nil, syscall.EINVAL
	}
	newPath := path.Join(parentPath, name)
	if err := s.store.Link(oldPath, newPath); err != nil {
		return nil, err
	}
	s.invalidateDirListings(parentPath)
	a, err := s.store.Stat(newPath)
	if err != nil {
		return nil, err
	}
	id, err := s.nodeOf(newPath, true)
	if err != nil {
		return nil, err
	}
	entry := s.entryOut(id, a)
	return encodeStruct(&entry), nil
}

// nameOf reads one NUL-terminated name from a request body. ok is false when
// the body holds no terminator: a name the sender never ended is malformed, and
// acting on the bytes that happen to follow would answer a request the kernel
// could not have sent. See TheoryOfFuseMount.
func nameOf(b []byte) (name string, ok bool) {
	n, _, ok := splitName(b)
	if !ok {
		return "", false
	}
	return string(n), true
}

// validEntryName reports whether name can name one directory entry: non-empty,
// not "." or "..", and free of the path separator. Joining any other name would
// walk out of the parent the request names, so the handlers refuse it.
// See TheoryOfFuseMount.
func validEntryName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '/' {
			return false
		}
	}
	return true
}

// entryName reads one NUL-terminated name that must name a single directory
// entry. ok is false for a name with no terminator, an empty name, "." or "..",
// or a name carrying the path separator. See TheoryOfFuseMount.
func entryName(b []byte) (name string, ok bool) {
	n, _, ok := splitName(b)
	if !ok {
		return "", false
	}
	name = string(n)
	return name, validEntryName(name)
}

// decodeStruct decodes one fixed request body into dst, zero-padding a body
// shorter than the struct so an older protocol minor parses cleanly.
func decodeStruct(dst any, body []byte) {
	size := binary.Size(dst)
	if size <= 0 {
		return
	}
	buf := body
	if len(buf) < size {
		padded := make([]byte, size)
		copy(padded, buf)
		buf = padded
	}
	_ = binary.Read(bytes.NewReader(buf[:size]), binary.LittleEndian, dst)
}

// fuseEntryOutSize and fuseAttrOutSize are the wire sizes of the two reply
// bodies every metadata request carries, measured once from the struct
// definitions so the direct encoder and the reflection fallback can never
// disagree. See TheoryOfFuseMount.
var (
	fuseEntryOutSize = binary.Size(fuseEntryOut{})
	fuseAttrOutSize  = binary.Size(fuseAttrOut{})
)

// encodeStruct encodes one response body. The two hot reply types — the entry
// and attribute bodies every lookup and getattr carries — encode through
// direct field writes, because a reply is built once per request and the
// reflection encoder costs more than the rest of a stat reply. The entry body
// has one writer, putEntryOut, which the READDIRPLUS batch calls in place.
// Every other type keeps the reflection encoder. The kernel parses a reply by
// its own struct offsets, so the direct path must preserve the exact wire
// order. See TheoryOfFuseMount.
func encodeStruct(src any) []byte {
	switch v := src.(type) {
	case *fuseEntryOut:
		if v == nil {
			return nil
		}
		buf := make([]byte, fuseEntryOutSize)
		putEntryOut(buf, v)
		return buf
	case *fuseAttrOut:
		if v == nil {
			return nil
		}
		buf := make([]byte, fuseAttrOutSize)
		binary.LittleEndian.PutUint64(buf[0:], v.AttrValid)
		binary.LittleEndian.PutUint32(buf[8:], v.AttrValidNsec)
		putFuseAttr(buf[16:], v.Attr)
		return buf
	}
	size := binary.Size(src)
	if size <= 0 {
		return nil
	}
	buf := make([]byte, size)
	_ = binary.Write(bytes.NewBuffer(buf[:0]), binary.LittleEndian, src)
	return buf
}

// putEntryOut writes one fuse_entry_out at the start of dst, which must hold
// at least fuseEntryOutSize bytes: the node id, the generation, the entry and
// attribute cache timeouts, and the attribute. It is the one writer of the
// entry body, shared by the reply encoder and the READDIRPLUS batch, so the
// wire layout cannot drift between them. See TheoryOfFuseMount and
// TheoryOfAttrCaching.
func putEntryOut(dst []byte, e *fuseEntryOut) {
	binary.LittleEndian.PutUint64(dst[0:], e.Nodeid)
	binary.LittleEndian.PutUint64(dst[8:], e.Generation)
	binary.LittleEndian.PutUint64(dst[16:], e.EntryValid)
	binary.LittleEndian.PutUint64(dst[24:], e.AttrValid)
	binary.LittleEndian.PutUint32(dst[32:], e.EntryValidNsec)
	binary.LittleEndian.PutUint32(dst[36:], e.AttrValidNsec)
	putFuseAttr(dst[40:], e.Attr)
}

// putFuseAttr writes one fuse_attr in the kernel's wire order: three 64-bit
// times, then the nanosecond, mode, and ownership fields. The offsets follow
// struct fuse_attr, the order the kernel reads by its own field offsets.
// See TheoryOfFuseMount.
func putFuseAttr(dst []byte, a fuseAttr) {
	binary.LittleEndian.PutUint64(dst[0:], a.Ino)
	binary.LittleEndian.PutUint64(dst[8:], a.Size)
	binary.LittleEndian.PutUint64(dst[16:], a.Blocks)
	binary.LittleEndian.PutUint64(dst[24:], a.Atime)
	binary.LittleEndian.PutUint64(dst[32:], a.Mtime)
	binary.LittleEndian.PutUint64(dst[40:], a.Ctime)
	binary.LittleEndian.PutUint32(dst[48:], a.Atimensec)
	binary.LittleEndian.PutUint32(dst[52:], a.Mtimensec)
	binary.LittleEndian.PutUint32(dst[56:], a.Ctimensec)
	binary.LittleEndian.PutUint32(dst[60:], a.Mode)
	binary.LittleEndian.PutUint32(dst[64:], a.Nlink)
	binary.LittleEndian.PutUint32(dst[68:], a.Uid)
	binary.LittleEndian.PutUint32(dst[72:], a.Gid)
	binary.LittleEndian.PutUint32(dst[76:], a.Rdev)
	binary.LittleEndian.PutUint32(dst[80:], a.Blksize)
	binary.LittleEndian.PutUint32(dst[84:], a.Flags)
}

var _ = fmt.Sprintf
