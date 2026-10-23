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

Node ids are paths. The protocol requires stable node ids, and the store is
path-addressed, so the server allocates numeric ids on first use and keeps
the mapping until the process exits. Paths are the identity; numeric ids
are only the wire form. LOOKUP of a fresh path allocates a new node id, and
LOOKUP of a known path returns the existing id. FORGET is accepted without a
reply and ignored: paths stay addressable for the session's life, matching
the store, which keeps every touched path in the audit trail and the
sqlite file.

Write-back happens on WRITE, not on flush: the store is a sqlite file, so
an open file's content lives in the store and a kernel WRITE persists the
new content immediately. Reads stream and writes materialize — a read
windows the stored blob or the base file, a write buffers the file from
first use and writes the whole content back. The trade-offs are one sqlite
transaction per WRITE and one in-memory copy per file opened for writing,
and the design's performance caveat covers both. Correctness first: a
killed process leaves a consistent store, and the review step sees every
byte the session wrote. See TheoryOfCompletePOSIX.

A request buffer is sized for the largest request the kernel may send: the
announced MaxWrite payload after the header and the write body. Sizing the
buffer at MaxWrite exactly truncates the payload of a maximal WRITE, which
is the shape a toolchain emits for every full block of a large file.

Passthrough directories bypass FUSE entirely. The design excludes /tmp, the
Go caches, and the user config directory from audit and review: their
content is machine-managed state whose interception would add cost without
adding review value. /proc, /sys, and /dev must be real kernel filesystems
because a read-through proxy cannot emulate them. They are bind-mounted
over the FUSE view before the pivot, so processes reach them directly.
See TheoryOfPassthrough.
`

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
directly.

The store's sqlite database is the one file that must not transit FUSE: the
FUSE daemon serves the store, so a store read that reached the daemon would
recurse. Its directory must therefore be one of the passthroughs, or the
database must be opened and its WAL files created before the pivot. The
mount helper documents the requirement on MountOptions.DBPath.
`

const (
	// fuseRootID is the node id the kernel assigns to the filesystem root.
	fuseRootID = 1

	// fuseMaxWrite bounds the read loop's buffer and one kernel write.
	fuseMaxWrite = 1 << 17

	// fuseProtocolMinor is the kernel protocol minor we speak: field
	// definitions through Linux 7.31.
	fuseProtocolMinor = 31

	// fuseAttrTimeoutSec keeps kernel attribute caches short: the store
	// can change under the kernel's feet, and a stale attribute is best
	// corrected quickly.
	fuseAttrTimeoutSec = 1

	fuseMaxTruncate = 64 << 20
)

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

// fuseOpFsyncdir is FUSE_FSYNCDIR. Tools commonly fsync a directory after
// creating or renaming entries to force the entry metadata to disk. The
// sandbox's directory metadata lives in sqlite, which persists writes at
// every WRITE and CREATE, so FSYNCDIR is a no-op — but answering it as
// successful instead of ENOSYS keeps the toolchain's durability path
// working. See TheoryOfCompletePOSIX.
const fuseOpFsyncdir = 32

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

// fuseInitOut mirrors struct fuse_init_out through protocol 7.31.
type fuseInitOut struct {
	Major               uint32
	Minor               uint32
	MaxReadahead        uint32
	Flags               uint32
	Flags2              uint32
	Pad                 uint32
	MaxBackground       uint16
	CongestionThreshold uint16
	MaxWrite            uint32
	TimeGran            uint32
	MaxPages            uint16
	MapAlignment        uint16
	Pad2                uint32
	Pad3                uint32
	Pad4                uint32
	Pad5                uint32
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

// fuseAccessIn mirrors struct fuse_access_in.
type fuseAccessIn struct {
	Mask uint32
	Pad  uint32
}

// fuseAccessOut mirrors struct fuse_access_out.
type fuseAccessOut struct {
	Mask uint32
	Pad  uint32
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

// fuseHandle is one open file or directory. A file's content is
// materialized on the first write only: a handle opened for reading never
// copies the file into memory. See TheoryOfCompletePOSIX.
type fuseHandle struct {
	path    string
	isDir   bool
	loaded  bool
	buf     []byte
	entries []DirEntry
}

type fuseServer struct {
	store *Store
	devFd int

	mu      sync.Mutex
	nodes   map[uint64]string
	byPath  map[string]uint64
	handles map[uint64]*fuseHandle
	nextID  uint64
	nextFh  uint64

	done chan struct{}
}

func newFuseServer(store *Store, devFd int) *fuseServer {
	return &fuseServer{
		store:   store,
		devFd:   devFd,
		nodes:   map[uint64]string{fuseRootID: "/"},
		byPath:  map[string]uint64{"/": fuseRootID},
		handles: make(map[uint64]*fuseHandle),
		nextID:  fuseRootID + 1,
		nextFh:  1,
		done:    make(chan struct{}),
	}
}

func (s *fuseServer) run() {
	defer close(s.done)
	hdrSize := binary.Size(fuseInHeader{})
	buf := make([]byte, fuseMaxWrite+binary.Size(fuseWriteIn{})+hdrSize)
	for {
		n, err := unix.Read(s.devFd, buf)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return
		}
		if n <= 0 || n < hdrSize {
			return
		}
		s.dispatch(buf[:n])
	}
}

func (s *fuseServer) dispatch(buf []byte) {
	var h fuseInHeader
	if err := binary.Read(bytes.NewReader(buf[:binary.Size(h)]), binary.LittleEndian, &h); err != nil {
		return
	}
	body := buf[binary.Size(h):]
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
		var in fuseAccessIn
		decodeStruct(&in, body)
		respBody, err = encodeStruct(&fuseAccessOut{Mask: in.Mask}), nil
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

func (s *fuseServer) respond(unique uint64, err error, body []byte) {
	errno := int32(0)
	if err != nil {
		errno = fuseErrno(err)
		body = nil
	}
	outLen := uint32(binary.Size(fuseOutHeader{}) + len(body))
	out := make([]byte, outLen)
	binary.LittleEndian.PutUint32(out[0:4], outLen)
	binary.LittleEndian.PutUint32(out[4:8], uint32(errno))
	binary.LittleEndian.PutUint64(out[8:16], unique)
	copy(out[binary.Size(fuseOutHeader{}):], body)
	_, _ = unix.Write(s.devFd, out)
}

// fuseErrno maps an error to the negative errno the kernel expects in a
// response header. A syscall.Errno carries its own code and wins over the
// sentinel checks: syscall.EPERM also satisfies errors.Is(err, os.ErrPermission),
// so testing the sentinels first would report EACCES where the operation
// reported EPERM — mknod of a device node is the case that surfaced it.
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
	if errors.Is(err, ErrUnsupported) {
		return -int32(syscall.ENOSYS)
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

// attrOfFuse renders one store attribute on the wire. A named pipe reports
// S_IFIFO so the kernel routes pipe opens to the pipe layer, never back to
// the filesystem. See TheoryOfCompletePOSIX.
func attrOfFuse(ino uint64, a Attr) fuseAttr {
	var mode uint32
	switch a.Kind {
	case KindDir:
		mode = syscall.S_IFDIR
	case KindSymlink:
		mode = syscall.S_IFLNK
	case KindFIFO:
		mode = syscall.S_IFIFO
	default:
		mode = syscall.S_IFREG
	}
	mode |= uint32(a.Mode.Perm())
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
		Uid: uint32(os.Getuid()), Gid: uint32(os.Getgid()),
		Blksize: 4096,
	}
}

func (s *fuseServer) entryOut(id uint64, a Attr) fuseEntryOut {
	return fuseEntryOut{
		Nodeid:     id,
		Generation: 1,
		EntryValid: fuseAttrTimeoutSec,
		AttrValid:  fuseAttrTimeoutSec,
		Attr:       attrOfFuse(id, a),
	}
}

func (s *fuseServer) handleInit(body []byte) ([]byte, error) {
	var in fuseInitIn
	decodeStruct(&in, body)
	minor := in.Minor
	if minor > fuseProtocolMinor {
		minor = fuseProtocolMinor
	}
	return encodeStruct(&fuseInitOut{
		Major:               in.Major,
		Minor:               minor,
		MaxReadahead:        in.MaxReadahead,
		MaxBackground:       12,
		CongestionThreshold: 9,
		MaxWrite:            fuseMaxWrite,
	}), nil
}

func (s *fuseServer) handleLookup(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	name := cstring(body)
	if name == "" || name == "." || name == ".." {
		return nil, syscall.ENOENT
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
	return encodeStruct(&[]fuseEntryOut{s.entryOut(id, a)}[0]), nil
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
		AttrValid: fuseAttrTimeoutSec,
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
		if in.Size > fuseMaxTruncate {
			return nil, syscall.EFBIG
		}
		// Read only the prefix the new size retains, then pad with
		// zeros when the file grows: the whole file never enters
		// memory for a truncate. See TheoryOfCompletePOSIX.
		content, err := s.store.ReadRange(p, 0, int(in.Size))
		if err != nil {
			return nil, err
		}
		truncated := make([]byte, in.Size)
		copy(truncated, content)
		perm := cur.Mode
		if perm == 0 {
			perm = 0o644
		}
		if err := s.store.WriteFile(p, truncated, perm); err != nil {
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
		AttrValid: fuseAttrTimeoutSec,
		Attr:      attrOfFuse(nodeid, a),
	}), nil
}

// invalidateHandles drops the buffered content of every open handle of p, so
// a write that follows a truncate reloads the store's new content instead of
// resurrecting the old bytes. See TheoryOfCompletePOSIX.
func (s *fuseServer) invalidateHandles(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.handles {
		if h.path == p {
			h.buf = nil
			h.loaded = false
		}
	}
}

// invalidateOtherHandles drops the buffered content of every open handle of
// p except the writing one. Two handles over one path both materialize their
// own buffer; without the drop, a write through handle A would leave handle
// B's stale buffer in place, and B's next write would overwrite A's bytes.
// The writing handle keeps its buffer: the content it just wrote is the
// current store content, so a reload would cost a read for nothing.
// See TheoryOfCompletePOSIX.
func (s *fuseServer) invalidateOtherHandles(p string, self uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for fh, h := range s.handles {
		if fh == self || h.path != p {
			continue
		}
		h.buf = nil
		h.loaded = false
	}
}

// handleOpen serves OPEN for a regular file. A write-mode open is checked
// against the write policy here, so the caller sees EACCES from open, not
// from the first write. O_TRUNC without O_CREAT is honoured: it arrives as
// OPEN rather than CREATE, and POSIX gives it the same truncating effect.
// See TheoryOfWritePolicy and TheoryOfCompletePOSIX.
func (s *fuseServer) handleOpen(nodeid uint64, body []byte) ([]byte, error) {
	var in fuseOpenIn
	decodeStruct(&in, body)
	p, err := s.pathOf(nodeid)
	if err != nil {
		return nil, err
	}
	writeMode := in.Flags&(uint32(unix.O_WRONLY)|uint32(unix.O_RDWR)) != 0
	if writeMode {
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
	truncated := false
	if writeMode &&
		in.Flags&uint32(unix.O_TRUNC) != 0 &&
		a.Kind == KindRegular {
		perm := a.Mode
		if perm == 0 {
			perm = 0o644
		}
		if err := s.store.WriteFile(p, nil, perm); err != nil {
			return nil, err
		}
		s.invalidateHandles(p)
		truncated = true
	}
	h := &fuseHandle{path: p}
	if truncated {
		// The store now holds an empty file, so a following write can
		// extend the empty buffer directly.
		h.loaded = true
	}
	s.mu.Lock()
	fh := s.nextFh
	s.nextFh++
	s.handles[fh] = h
	s.mu.Unlock()
	return encodeStruct(&fuseOpenOut{Fh: fh}), nil
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
	return s.store.ReadRange(h.path, int64(in.Offset), int(in.Size))
}

func (s *fuseServer) handleWrite(body []byte) ([]byte, error) {
	var in fuseWriteIn
	decodeStruct(&in, body)
	hdrSize := binary.Size(in)
	if len(body) < hdrSize+int(in.Size) {
		return nil, syscall.EINVAL
	}
	// A zero-byte write is a no-op, matching Linux: write(2) with nbyte=0
	// transfers nothing and does not extend the file, even when the offset
	// sits past the end. See TheoryOfCompletePOSIX.
	if in.Size == 0 {
		return encodeStruct(&fuseWriteOut{Size: 0}), nil
	}
	// A malicious or buggy kernel could send an offset that overflows
	// the int conversion; refuse before allocating. The window is
	// bounded by fuseMaxTruncate for the same reason the truncate path
	// is.
	if in.Offset > uint64(fuseMaxTruncate) ||
		in.Offset+uint64(in.Size) > uint64(fuseMaxTruncate) {
		return nil, syscall.EFBIG
	}
	data := body[hdrSize : hdrSize+int(in.Size)]
	s.mu.Lock()
	h := s.handles[in.Fh]
	s.mu.Unlock()
	if h == nil {
		return nil, syscall.EBADF
	}
	if err := s.ensureHandleLoaded(h); err != nil {
		return nil, err
	}
	end := int(in.Offset) + len(data)
	if end > len(h.buf) {
		next := make([]byte, end)
		copy(next, h.buf)
		h.buf = next
	}
	copy(h.buf[in.Offset:], data)
	perm := os.FileMode(0o644)
	if a, err := s.store.Stat(h.path); err == nil && a.Mode != 0 {
		perm = a.Mode
	}
	if err := s.store.WriteFile(h.path, h.buf, perm); err != nil {
		return nil, err
	}
	// A write through one handle is now the current store content; other
	// handles over the same path must drop their stale buffers so their
	// next write reloads instead of overwriting this one.
	// See invalidateOtherHandles.
	s.invalidateOtherHandles(h.path, in.Fh)
	return encodeStruct(&fuseWriteOut{Size: in.Size}), nil
}

// ensureHandleLoaded materializes a write handle's buffer from the store on
// first use, so a handle opened for reading never copies a file into
// memory. See TheoryOfCompletePOSIX.
func (s *fuseServer) ensureHandleLoaded(h *fuseHandle) error {
	if h.loaded {
		return nil
	}
	content, err := s.store.ReadFile(h.path)
	if err != nil {
		return err
	}
	h.buf = content
	h.loaded = true
	return nil
}

func (s *fuseServer) handleRelease(body []byte) ([]byte, error) {
	var in fuseReleaseIn
	decodeStruct(&in, body)
	s.mu.Lock()
	delete(s.handles, in.Fh)
	s.mu.Unlock()
	return nil, nil
}

func (s *fuseServer) handleOpendir(nodeid uint64) ([]byte, error) {
	p, err := s.pathOf(nodeid)
	if err != nil {
		return nil, err
	}
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
	all = append(all, entries...)
	s.mu.Lock()
	fh := s.nextFh
	s.nextFh++
	s.handles[fh] = &fuseHandle{path: p, isDir: true, entries: all}
	s.mu.Unlock()
	return encodeStruct(&fuseOpenOut{Fh: fh}), nil
}

// handleReaddir encodes dirents from the offset the kernel supplies.
// See TheoryOfFuseMount.
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
	if start < 0 || start > len(h.entries) {
		return []byte{}, nil
	}
	const direntHeader = 24
	maxSize := int(in.Size)
	out := make([]byte, 0, maxSize)
	for i := start; i < len(h.entries); i++ {
		e := h.entries[i]
		entrySize := direntHeader + len(e.Name)
		padded := (entrySize + 7) &^ 7
		if len(out)+padded > maxSize {
			break
		}
		buf := make([]byte, padded)
		binary.LittleEndian.PutUint64(buf[0:], uint64(i+2))
		binary.LittleEndian.PutUint64(buf[8:], uint64(i+1))
		binary.LittleEndian.PutUint32(buf[16:], uint32(len(e.Name)))
		binary.LittleEndian.PutUint32(buf[20:], direntType(e.Attr.Kind))
		copy(buf[direntHeader:], e.Name)
		out = append(out, buf...)
	}
	return out, nil
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

// handleCreate creates or opens a path for a create-with-open request. The
// kernel may name an existing path, so O_EXCL and O_TRUNC are honoured like
// open(2): without O_TRUNC an existing file keeps its content. An existing
// directory reports EISDIR — a create-with-open hands back a writable handle,
// and a directory has no writable content — matching open(2) with O_WRONLY
// on a directory. See TheoryOfCompletePOSIX.
func (s *fuseServer) handleCreate(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	var in fuseCreateIn
	decodeStruct(&in, body)
	name := cstring(body[binary.Size(in):])
	if name == "" || name == "." || name == ".." {
		return nil, syscall.EINVAL
	}
	p := path.Join(parentPath, name)
	perm := os.FileMode(in.Mode & 0o777)
	if perm == 0 {
		perm = 0o644
	}
	refreshed := false
	cur, statErr := s.store.Stat(p)
	switch {
	case statErr == nil:
		if in.Flags&uint32(unix.O_EXCL) != 0 {
			return nil, ErrExist
		}
		if cur.Kind == KindDir {
			return nil, syscall.EISDIR
		}
		if in.Flags&uint32(unix.O_TRUNC) != 0 {
			if err := s.store.WriteFile(p, nil, perm); err != nil {
				return nil, err
			}
			refreshed = true
		} else if err := s.store.CanWrite(p); err != nil {
			return nil, err
		}
	case errors.Is(statErr, ErrNotExist):
		if err := s.store.WriteFile(p, nil, perm); err != nil {
			return nil, err
		}
		refreshed = true
	default:
		return nil, statErr
	}
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	id, err := s.nodeOf(p, true)
	if err != nil {
		return nil, err
	}
	h := &fuseHandle{path: p}
	if refreshed {
		// The store now holds an empty file for this handle, so a
		// following write can extend the empty buffer directly.
		h.loaded = true
	}
	s.mu.Lock()
	fh := s.nextFh
	s.nextFh++
	s.handles[fh] = h
	s.mu.Unlock()
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
	name := cstring(body[binary.Size(in):])
	if name == "" || name == "." || name == ".." {
		return nil, syscall.EINVAL
	}
	p := path.Join(parentPath, name)
	perm := os.FileMode(in.Mode & 0o777)
	if perm == 0 {
		perm = 0o755
	}
	if err := s.store.Mkdir(p, perm); err != nil {
		return nil, err
	}
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	id, err := s.nodeOf(p, true)
	if err != nil {
		return nil, err
	}
	return encodeStruct(&[]fuseEntryOut{s.entryOut(id, a)}[0]), nil
}

// handleMknod creates the special files a task asks for. A named pipe is the
// only kind the store records: device nodes belong to the mount layer's
// passthrough view and sockets carry no filesystem content, so both report
// EPERM instead of recording a node the sandbox cannot serve.
// See TheoryOfCompletePOSIX.
func (s *fuseServer) handleMknod(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	var in fuseMknodIn
	decodeStruct(&in, body)
	name := cstring(body[binary.Size(in):])
	if name == "" || name == "." || name == ".." {
		return nil, syscall.EINVAL
	}
	if in.Mode&uint32(syscall.S_IFMT) != uint32(syscall.S_IFIFO) {
		return nil, syscall.EPERM
	}
	p := path.Join(parentPath, name)
	perm := os.FileMode(in.Mode & 0o777)
	if perm == 0 {
		perm = 0o644
	}
	if err := s.store.Mkfifo(p, perm); err != nil {
		return nil, err
	}
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
// See TheoryOfCompletePOSIX.
func (s *fuseServer) handleUnlink(parent uint64, body []byte, isDir bool) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	name := cstring(body)
	if name == "" || name == "." || name == ".." {
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
	return nil, s.store.Remove(p)
}

// handleRename serves RENAME and RENAME2. RENAME2's flags select the
// semantic: RENAME_NOREPLACE refuses an existing destination with EEXIST,
// and RENAME_EXCHANGE and RENAME_WHITEOUT need an atomic inode swap the
// path-addressed store cannot provide, so they are refused with ENOSYS
// and the caller falls back to the plain form. The three flag bits mirror
// linux/fs.h; the wire protocol carries them as a uint32.
// See TheoryOfCompletePOSIX.
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
	newParent, err := s.pathOf(newdir)
	if err != nil {
		return nil, err
	}
	rest := body[hdrSize:]
	oldName := cstring(rest)
	if oldName == "" {
		return nil, syscall.EINVAL
	}
	rest = rest[len(oldName)+1:]
	newName := cstring(rest)
	if newName == "" {
		return nil, syscall.EINVAL
	}
	oldPath := path.Join(parentPath, oldName)
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
		if _, err := s.store.Stat(newPath); err == nil {
			return nil, ErrExist
		}
	}
	return nil, s.store.Rename(oldPath, newPath)
}

// handleSymlink creates a symbolic link. The kernel wire form is
// link_name\0link_target\0 — the name first, matching FUSE_SYMLINK's
// documented fields — so the two strings decode in that order.
// See TheoryOfCompletePOSIX.
func (s *fuseServer) handleSymlink(parent uint64, body []byte) ([]byte, error) {
	parentPath, err := s.pathOf(parent)
	if err != nil {
		return nil, err
	}
	name := cstring(body)
	if name == "" || name == "." || name == ".." {
		return nil, syscall.EINVAL
	}
	rest := body[len(name)+1:]
	target := cstring(rest)
	if target == "" {
		return nil, syscall.EINVAL
	}
	p := path.Join(parentPath, name)
	if err := s.store.Symlink(target, p); err != nil {
		return nil, err
	}
	a, err := s.store.Stat(p)
	if err != nil {
		return nil, err
	}
	id, err := s.nodeOf(p, true)
	if err != nil {
		return nil, err
	}
	return encodeStruct(&[]fuseEntryOut{s.entryOut(id, a)}[0]), nil
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
	oldPath, err := s.pathOf(in.Oldnodeid)
	if err != nil {
		return nil, err
	}
	name := cstring(body[binary.Size(in):])
	if name == "" || name == "." || name == ".." {
		return nil, syscall.EINVAL
	}
	newPath := path.Join(parentPath, name)
	if err := s.store.Link(oldPath, newPath); err != nil {
		return nil, err
	}
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

// cstring reads a NUL-terminated name from a request body.
func cstring(b []byte) string {
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			return string(b[:i])
		}
	}
	return string(b)
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

// encodeStruct encodes one response body.
func encodeStruct(src any) []byte {
	size := binary.Size(src)
	if size <= 0 {
		return nil
	}
	buf := make([]byte, size)
	_ = binary.Write(bytes.NewBuffer(buf[:0]), binary.LittleEndian, src)
	return buf
}

var _ = fmt.Sprintf
