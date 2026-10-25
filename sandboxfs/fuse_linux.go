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
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const TheoryOfFuseMount = `
The sandbox is mounted as a FUSE filesystem, not emulated in-process. FUSE
is the only mechanism that routes every file operation of every process —
the toolchain, shell commands, the change-block applier — through one
userspace handler, so the store's audit and its sqlite upper layer cover
all I/O without each consumer knowing they exist. A userspace-only
abstraction would cover only the code paths that opt into it, which
defeats the design.

The implementation speaks the kernel protocol directly instead of a third-
party library: the protocol is small (a request header, an opcode, and
opcode-specific fixed bodies), the kernel ABI is stable, and one dependency
fewer keeps the sandbox inside the module's existing constraints. The
handler is driven synchronously: read one request, dispatch it, write one
response. The kernel serializes requests per connection, so no worker pool
is needed; a slow handler delays later requests but not correctness.

The read loop separates the errors that do not end the daemon from the ones
that do. EINTR says a signal arrived. ENOENT says the caller interrupted its
request before this server read it, and the answer is to read the next
request. Returning on either stops the server, and from then on every
filesystem call through the mount fails with EIO.

A request body carries its fixed header followed by its names. A body
shorter than the fixed header, or a name with no terminator, is malformed:
the handler reports EINVAL rather than slicing past the body, because a
panic on the server's single goroutine kills the daemon for every later
request. The decodeStruct padding covers the fixed fields only; the names
come from the raw length.

The request header's length field is checked before the body is sliced. The
read buffer is reused, so a length that claims bytes the read did not
deliver would hand the handler the previous request's bytes. A length below
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
the store, which keeps every touched path in the audit trail and the
sqlite file.

Write-back happens on WRITE, not on flush: the store is a sqlite file, so
an open file's content lives in the store and a kernel WRITE persists the
new content immediately. A killed process therefore leaves a consistent
store, and the review step sees every byte the session wrote. The read and
write paths own the mechanism (see TheoryOfReadPath and
TheoryOfWriteSplice); the POSIX-visible rules are in TheoryOfCompletePOSIX.

A request buffer is sized for the largest request the kernel may send: the
announced MaxWrite payload after the header and the write body. Sizing the
buffer at MaxWrite exactly truncates the payload of a maximal WRITE, which
is the shape a toolchain emits for every full block of a large file.

Passthrough directories bypass FUSE entirely. See TheoryOfPassthrough.
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

The store's sqlite database is the one file that must not transit FUSE: the
FUSE daemon serves the store, so a store read that reached the daemon would
recurse. Its directory must therefore be one of the passthroughs, or the
database must be opened and its WAL files created before the pivot. The
mount helper documents the requirement on MountOptions.DBPath.
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

// FUSE INIT capability bits, from include/uapi/linux/fuse.h. The server echoes
// only the bits the kernel offers. See TheoryOfInitNegotiation.
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

// fuseHandle is one open file or directory. A file's content lives only
// in the store: a write splices its delta there, so the handle holds no
// copy of the file and a large open file costs no memory. A read handle
// caches the base-layer file it opened, so streaming one base file costs
// one open and one type check per handle instead of one of each per window.
// See TheoryOfWriteSplice and TheoryOfReadPath.
type fuseHandle struct {
	path    string
	isDir   bool
	entries []DirEntry

	// mode is the file's permission bits, captured at open or create, so the
	// write path needs no metadata lookup of its own: the open that handed
	// the handle back already read the attribute. See TheoryOfWriteSplice.
	mode os.FileMode

	// readBuf is the handle's read window buffer, reused across reads: the
	// server answers one request at a time and the reply reaches the kernel
	// before the next request is read, so a streamed file costs one buffer per
	// handle instead of one allocation per window. See TheoryOfReadPath.
	readBuf []byte

	// listBuf is the directory listing's reply scratch, reused across
	// READDIR and READDIRPLUS calls for the same reason readBuf is reused:
	// the reply reaches the kernel before the next request is read, so a
	// repeated listing of one directory allocates nothing after its first
	// call. Both handlers size it from the entries that fit instead of the
	// announced window, which can be far larger than a small directory.
	// See TheoryOfInitNegotiation.
	listBuf []byte

	// base is the handle's cached base-layer reader: opened lazily on the
	// first read the upper layer does not answer, closed on release or
	// invalidation. See TheoryOfReadPath.
	base *os.File
}

// closeBase releases the handle's cached base-layer reader, if any. The
// server is single-threaded, so no read is in flight while it runs.
// See TheoryOfReadPath.
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

	mu      sync.Mutex
	nodes   map[uint64]string
	byPath  map[string]uint64
	handles map[uint64]*fuseHandle
	nextID  uint64
	nextFh  uint64

	// outBuf is the response scratch the server reuses for every reply. The
	// server answers one request at a time, so one buffer serves all responses
	// and a reply costs no allocation.
	outBuf []byte

	// timings aggregates the measured cost of every answered request, so a
	// session can report which operation its workload spends its time in.
	// The constructor installs it and the read loop stamps every request; a
	// nil value measures nothing, which is what a server built without the
	// constructor gets. See TheoryOfOperationTiming.
	timings *OpTimings

	done chan struct{}
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
	// The buffer holds the largest request the kernel may send after the
	// negotiation: the request header, one write header, and a full
	// fuseMaxWriteMax payload. See TheoryOfInitNegotiation.
	buf := make([]byte, fuseInHeaderSize+binary.Size(fuseWriteIn{})+fuseMaxWriteMax)
	for {
		n, err := s.readReq(buf)
		if err != nil {
			// Two read errors do not end the daemon. EINTR says a signal
			// arrived. ENOENT says the caller interrupted its request before
			// this server read it; the FUSE device asks for the next request.
			// Returning on either kills the daemon and every later filesystem
			// call fails with EIO. See TheoryOfFuseMount.
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.ENOENT) {
				continue
			}
			return
		}
		if n <= 0 || n < fuseInHeaderSize {
			return
		}
		// One start stamp and one record per request attribute the server's
		// own cost — dispatch plus the reply write — to the request's opcode,
		// and the request joins the flight ring the moment it finishes.
		// See TheoryOfOperationTiming and TheoryOfFlightRecorder.
		opcode := binary.LittleEndian.Uint32(buf[4:8])
		nodeid := binary.LittleEndian.Uint64(buf[16:24])
		mark := s.timings.markRequest(nodeid)
		s.dispatch(buf[:n])
		s.timings.recordRequest(opcode, mark)
	}
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
	// The header's Len field is the request's own length. A length shorter
	// than the header or longer than the bytes read describes a body that is
	// not there, and the reused request buffer still holds the previous
	// request's bytes beyond the read: parsing them would answer a request
	// the kernel never sent. A zero length means the sender left the field
	// empty, so the whole read stands as the body. See TheoryOfFuseMount.
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

// respond writes one reply. The header and the payload leave as one writev:
// a read reply can carry a whole negotiated request window, and copying that
// payload behind the header would move the bytes a second time per read. The
// kernel consumes one write per reply, and writev is one write. The header
// buffer is the server's reused scratch, so a reply costs no allocation.
// See TheoryOfFuseMount.
func (s *fuseServer) respond(unique uint64, err error, body []byte) {
	errno := int32(0)
	if err != nil {
		errno = fuseErrno(err)
		body = nil
	}
	if cap(s.outBuf) < fuseOutHeaderSize {
		s.outBuf = make([]byte, fuseOutHeaderSize)
	}
	out := s.outBuf[:fuseOutHeaderSize]
	binary.LittleEndian.PutUint32(out[0:4], uint32(fuseOutHeaderSize+len(body)))
	binary.LittleEndian.PutUint32(out[4:8], uint32(errno))
	binary.LittleEndian.PutUint64(out[8:16], unique)
	if len(body) == 0 {
		_, _ = unix.Write(s.devFd, out)
		return
	}
	_, _ = unix.Writev(s.devFd, [][]byte{out, body})
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
// the slice. The FUSE handler runs on the server's single goroutine, and a
// panic there kills the daemon for every later request. See TheoryOfFuseMount.
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
// number for the entry's d_ino and the path's st_ino. See TheoryOfFuseMount.
func childPath(parent, name string) string {
	switch name {
	case ".":
		return parent
	case "..":
		return path.Dir(parent)
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
// mustExist is true the path is stat'ed first, so an id never names a path
// the store does not hold.
func (s *fuseServer) nodeOf(p string, mustExist bool) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byPath[p]; ok {
		return id, nil
	}
	if mustExist {
		if _, err := s.store.Stat(p); err != nil {
			return 0, err
		}
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
// fuseMaxWriteMax. Only bits the kernel offers are echoed, so an older kernel
// keeps the conservative window. The handshake also fixes the protocol
// version: a newer kernel is told to fall back to fuseProtocolMajor, and an
// older one fails with EPROTO. See TheoryOfInitNegotiation.
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
		MaxBackground:       12,
		CongestionThreshold: 9,
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
	p := path.Join(parentPath, name)
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

// invalidateHandles drops the cached base reader of every open handle of p, so
// a read that follows a truncation serves the store's new content instead of a
// descriptor that fixes the old view. The store is the only copy of the
// content, so no other state needs dropping, and handle numbers start at 1, so
// a self of 0 names no handle and the loop reaches all of them.
// See TheoryOfReadPath.
func (s *fuseServer) invalidateHandles(p string) {
	s.invalidateOtherHandles(p, 0)
}

// invalidateOtherHandles drops the cached base reader of every open handle of p
// except the writing one, so their next read serves the store's now-current
// content. Handles hold no buffer, so a write through one handle cannot be
// overwritten by another handle's stale bytes; only the base view must go. A
// self of 0 names no handle, so the truncation path reuses this loop through
// invalidateHandles. See TheoryOfWriteSplice and TheoryOfReadPath.
func (s *fuseServer) invalidateOtherHandles(p string, self uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for fh, h := range s.handles {
		if fh == self || h.path != p {
			continue
		}
		h.closeBase()
	}
}

// invalidateDirListings clears the cached entries of every open handle of a
// directory, so a read that follows a mutation fills the handle with the
// entries the store holds now. The kernel cannot see a change this mount makes
// on its own: FOPEN_CACHE_DIR keeps the listing it read, and no notify call
// tells it otherwise, so the server clears the snapshot its own handler
// mutated. A continued read keeps the one snapshot its offsets name, so the
// rebuild happens only after a mutation, never in the middle of a listing.
// See TheoryOfOpenCaching and TheoryOfReadPath.
func (s *fuseServer) invalidateDirListings(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.handles {
		if h.isDir && h.path == p {
			h.entries = nil
		}
	}
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

// baseHandleFile returns the handle's cached base-layer reader, opening it on
// first use. The base descriptor answers the open with one fstatat and one
// openat: the kind check and the open are the read window's own pair, so a
// streamed file pays no component-by-component guard per open. A directory,
// link, or pipe has no readable content, and a pipe is never opened, so a pipe
// read never blocks. The caller has already established that the upper layer
// holds no window for the path. A store without the descriptor keeps the same
// kind check through the portable walk, so the refusal and the no-open rule
// hold on both paths. See TheoryOfReadPath and TheoryOfBaseFdAccess.
func (s *fuseServer) baseHandleFile(h *fuseHandle) (*os.File, error) {
	if h.base != nil {
		return h.base, nil
	}
	rel := relPath(h.path)
	f, fast, err := baseOpenFile(s.store.baseFd, rel)
	if !fast {
		// The platform offers no base descriptor, so the kind check and the
		// open run through the portable walk. The check must precede the open:
		// an open of a pipe blocks the server's one goroutine for every later
		// request, and an open of a link would serve its target's bytes.
		// See TheoryOfBaseFdAccess.
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
	}
	return nil, nil
}

func (s *fuseServer) handleOpendir(nodeid uint64) ([]byte, error) {
	p, err := s.pathOf(nodeid)
	if err != nil {
		return nil, err
	}
	all, err := s.listEntries(p)
	if err != nil {
		return nil, err
	}
	fh := s.registerHandle(&fuseHandle{path: p, isDir: true, entries: all})
	// The kernel keeps the listing across opens, so a repeated walk costs no
	// request; a mutation through this mount clears the snapshot, and the
	// next read rebuilds it. See TheoryOfOpenCaching and TheoryOfReadPath.
	return encodeStruct(&fuseOpenOut{Fh: fh, OpenFlags: fuseOpenCacheDir}), nil
}

// listEntries returns the directory's entries with "." and "..": the listing a
// READDIR reply carries. The store merges the two layers, and the parent comes
// from the store so a sandbox path's ".." reports the sandbox's own attributes.
// See TheoryOfReadPath.
func (s *fuseServer) listEntries(p string) ([]DirEntry, error) {
	entries, err := s.store.ReadDir(p)
	if err != nil {
		return nil, err
	}
	all := make([]DirEntry, 0, len(entries)+2)
	if self, err := s.store.Stat(p); err == nil {
		all = append(all, DirEntry{Name: ".", Attr: self})
	}
	if parent, err := s.store.Stat(path.Dir(p)); err == nil {
		all = append(all, DirEntry{Name: "..", Attr: parent})
	}
	return append(all, entries...), nil
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
	if h.entries == nil {
		// A mutation through this mount cleared the snapshot: the kernel
		// cannot see such a change, so the server rebuilds it from the store,
		// the one source of the directory's current entries.
		// See TheoryOfReadPath.
		all, err := s.listEntries(h.path)
		if err != nil {
			return nil, err
		}
		h.entries = all
	}
	start := int(in.Offset)
	if start < 0 || start > len(h.entries) {
		return []byte{}, nil
	}
	maxSize := int(in.Size)
	// The announced window is the negotiated 1 MiB even for a small
	// directory, so sizing the scratch from it would reserve far more than
	// the reply uses. Sum the entries that can fit instead.
	need := 0
	for i := start; i < len(h.entries); i++ {
		need += (fuseDirentSize + len(h.entries[i].Name) + 7) &^ 7
		if need >= maxSize {
			need = maxSize
			break
		}
	}
	if cap(h.listBuf) < need {
		h.listBuf = make([]byte, 0, need)
	}
	out := h.listBuf[:0]
	// One acquisition of the node table serves the whole reply, and each
	// entry's path comes from appending to the canonical parent. The dirent's
	// inode number is the node id the kernel gets from a LOOKUP of the same
	// name: a tool that compares a listing's d_ino with a stat's st_ino must
	// see one number, not two. See TheoryOfFuseMount.
	s.mu.Lock()
	for i := start; i < len(h.entries); i++ {
		e := h.entries[i]
		padded := (fuseDirentSize + len(e.Name) + 7) &^ 7
		if len(out)+padded > maxSize {
			break
		}
		id := s.childNodeLocked(h.path, e.Name)
		prev := len(out)
		out = out[:prev+padded]
		// The scratch is reused, so the name's trailing padding would
		// otherwise carry the previous reply's bytes.
		clear(out[prev:])
		binary.LittleEndian.PutUint64(out[prev:], id)
		binary.LittleEndian.PutUint64(out[prev+8:], uint64(i+1))
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
	if h.entries == nil {
		// The listing was cleared by a mutation through this mount: the
		// server rebuilds it from the store, the one source of the
		// directory's current entries. See TheoryOfReadPath.
		all, err := s.listEntries(h.path)
		if err != nil {
			return nil, err
		}
		h.entries = all
	}
	start := int(in.Offset)
	if start < 0 || start > len(h.entries) {
		return []byte{}, nil
	}
	maxSize := int(in.Size)
	// The announced window is the negotiated 1 MiB even for a small
	// directory, so sizing the scratch from it would reserve far more than
	// the reply uses. Sum the entries that can fit instead.
	need := 0
	for i := start; i < len(h.entries); i++ {
		need += (fuseEntryOutSize + fuseDirentSize + len(h.entries[i].Name) + 7) &^ 7
		if need >= maxSize {
			need = maxSize
			break
		}
	}
	if cap(h.listBuf) < need {
		h.listBuf = make([]byte, 0, need)
	}
	out := h.listBuf[:0]
	// One acquisition of the node table serves the whole reply, and each
	// entry's path comes from appending to the canonical parent, which also
	// resolves "." and ".." onto the directory and its parent. The dirent
	// carries the same node id the entry_out above announces: one record must
	// not name two inodes. See TheoryOfFuseMount.
	s.mu.Lock()
	for i := start; i < len(h.entries); i++ {
		e := h.entries[i]
		padded := (fuseEntryOutSize + fuseDirentSize + len(e.Name) + 7) &^ 7
		if len(out)+padded > maxSize {
			break
		}
		id := s.childNodeLocked(h.path, e.Name)
		prev := len(out)
		out = out[:prev+padded]
		// The scratch is reused, so the name's trailing padding would
		// otherwise carry the previous reply's bytes.
		clear(out[prev:])
		entry := s.entryOut(id, e.Attr)
		putEntryOut(out[prev:], &entry)
		binary.LittleEndian.PutUint64(out[prev+fuseEntryOutSize:], id)
		binary.LittleEndian.PutUint64(out[prev+fuseEntryOutSize+8:], uint64(i+1))
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
