//go:build linux

package sandboxfs

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type fuseClient struct {
	fd     int
	file   *os.File
	reader *bufio.Reader
	unique uint64
}

// newFuseClient wires a store to one end of a SEQPACKET socketpair and runs
// the FUSE server on the other end, so the test speaks the kernel protocol
// without a kernel mount. The socket buffers hold a maximal write message.
func newFuseClient(t *testing.T, store *Store) *fuseClient {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET, 0)
	if err != nil {
		t.Fatal(err)
	}
	size := fuseMaxWrite * 2
	_ = unix.SetsockoptInt(fds[0], unix.SOL_SOCKET, unix.SO_RCVBUF, size)
	_ = unix.SetsockoptInt(fds[1], unix.SOL_SOCKET, unix.SO_SNDBUF, size)
	server := newFuseServer(store, fds[0])
	go server.run()
	cf := os.NewFile(uintptr(fds[1]), "sandboxfs-fuse-client")
	t.Cleanup(func() {
		_ = cf.Close()
		<-server.done
		_ = unix.Close(fds[0])
	})
	return &fuseClient{fd: fds[1], file: cf, reader: bufio.NewReader(cf)}
}

func (c *fuseClient) request(t *testing.T, opcode uint32, nodeid uint64, body []byte) (int32, []byte) {
	t.Helper()
	c.unique++
	h := fuseInHeader{
		Len:    uint32(binary.Size(fuseInHeader{}) + len(body)),
		Opcode: opcode,
		Unique: c.unique,
		Nodeid: nodeid,
	}
	hdr := make([]byte, binary.Size(h))
	if err := binary.Write(bytes.NewBuffer(hdr[:0]), binary.LittleEndian, &h); err != nil {
		t.Fatal(err)
	}
	msg := append(hdr, body...)
	if _, err := unix.Write(c.fd, msg); err != nil {
		t.Fatal(err)
	}
	outHdr := make([]byte, binary.Size(fuseOutHeader{}))
	if _, err := io.ReadFull(c.reader, outHdr); err != nil {
		t.Fatal(err)
	}
	var oh fuseOutHeader
	if err := binary.Read(bytes.NewReader(outHdr), binary.LittleEndian, &oh); err != nil {
		t.Fatal(err)
	}
	if oh.Unique != c.unique {
		t.Fatalf("unique: got %d want %d", oh.Unique, c.unique)
	}
	bodyLen := int(oh.Len) - binary.Size(fuseOutHeader{})
	if bodyLen <= 0 {
		return oh.Error, nil
	}
	rest := make([]byte, bodyLen)
	if _, err := io.ReadFull(c.reader, rest); err != nil {
		t.Fatal(err)
	}
	return oh.Error, rest
}

func (c *fuseClient) bodyWithNames(prefix []byte, names ...string) []byte {
	out := append([]byte{}, prefix...)
	for _, n := range names {
		out = append(out, []byte(n)...)
		out = append(out, 0)
	}
	return out
}

func TestFuseServerWireProtocol(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "a.txt", "hello")

	c := newFuseClient(t, store)

	// INIT.
	initIn := fuseInitIn{Major: 7, Minor: 31}
	errno, resp := c.request(t, fuseOpInit, 0, encodeStruct(&initIn))
	if errno != 0 {
		t.Fatalf("init errno %d", errno)
	}
	var initOut fuseInitOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &initOut); err != nil {
		t.Fatal(err)
	}
	if initOut.Major != 7 || initOut.Minor != 31 {
		t.Fatalf("init out %+v", initOut)
	}

	// LOOKUP /a.txt.
	errno, resp = c.request(t, fuseOpLookup, 1, c.bodyWithNames(nil, "a.txt"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Attr.Size != 5 {
		t.Fatalf("lookup size %d", entry.Attr.Size)
	}
	aNode := entry.Nodeid

	// GETATTR.
	errno, resp = c.request(t, fuseOpGetattr, aNode, nil)
	if errno != 0 {
		t.Fatalf("getattr errno %d", errno)
	}

	// OPEN + READ.
	errno, resp = c.request(t, fuseOpOpen, aNode, encodeStruct(&fuseOpenIn{}))
	if errno != 0 {
		t.Fatalf("open errno %d", errno)
	}
	var openOut fuseOpenOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &openOut); err != nil {
		t.Fatal(err)
	}
	errno, resp = c.request(t, fuseOpRead, aNode, encodeStruct(&fuseReadIn{
		Fh: openOut.Fh, Offset: 1, Size: 3,
	}))
	if errno != 0 {
		t.Fatalf("read errno %d", errno)
	}
	if string(resp) != "ell" {
		t.Fatalf("read %q", resp)
	}
	if errno, _ := c.request(t, fuseOpRelease, aNode, encodeStruct(&fuseReleaseIn{Fh: openOut.Fh})); errno != 0 {
		t.Fatalf("release errno %d", errno)
	}

	// CREATE + WRITE.
	createIn := fuseCreateIn{Flags: 0o101, Mode: 0o644}
	errno, resp = c.request(t, fuseOpCreate, 1, c.bodyWithNames(encodeStruct(&createIn), "b.txt"))
	if errno != 0 {
		t.Fatalf("create errno %d", errno)
	}
	var createOut fuseCreateOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &createOut); err != nil {
		t.Fatal(err)
	}
	payload := []byte("world")
	writeIn := fuseWriteIn{Fh: createOut.Open.Fh, Offset: 0, Size: uint32(len(payload))}
	writeBody := append(encodeStruct(&writeIn), payload...)
	errno, resp = c.request(t, fuseOpWrite, createOut.Entry.Nodeid, writeBody)
	if errno != 0 {
		t.Fatalf("write errno %d", errno)
	}
	got, err := store.ReadFile("/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "world" {
		t.Fatalf("write round-trip %q", got)
	}

	// SETATTR truncate.
	errno, _ = c.request(t, fuseOpSetattr, createOut.Entry.Nodeid, encodeStruct(&fuseSetattrIn{
		Valid: fattrSize, Size: 0,
	}))
	if errno != 0 {
		t.Fatalf("setattr errno %d", errno)
	}
	got, err = store.ReadFile("/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("truncate left %q", got)
	}

	// MKDIR + OPENDIR + READDIR.
	mkdirIn := fuseMkdirIn{Mode: 0o755}
	errno, _ = c.request(t, fuseOpMkdir, 1, c.bodyWithNames(encodeStruct(&mkdirIn), "dir"))
	if errno != 0 {
		t.Fatalf("mkdir errno %d", errno)
	}
	errno, resp = c.request(t, fuseOpOpendir, 1, encodeStruct(&fuseOpenIn{}))
	if errno != 0 {
		t.Fatalf("opendir errno %d", errno)
	}
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &openOut); err != nil {
		t.Fatal(err)
	}
	errno, resp = c.request(t, fuseOpReaddir, 1, encodeStruct(&fuseReadIn{
		Fh: openOut.Fh, Offset: 0, Size: 4096,
	}))
	if errno != 0 {
		t.Fatalf("readdir errno %d", errno)
	}
	names := parseDirentNames(t, resp)
	for _, want := range []string{".", "..", "a.txt", "b.txt", "dir"} {
		if !contains(names, want) {
			t.Fatalf("readdir missing %q in %v", want, names)
		}
	}

	// UNLINK /a.txt whiteouts the base file.
	errno, _ = c.request(t, fuseOpUnlink, 1, c.bodyWithNames(nil, "a.txt"))
	if errno != 0 {
		t.Fatalf("unlink errno %d", errno)
	}
	if _, err := store.Stat("/a.txt"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("want whiteout, got err %v", err)
	}

	// RENAME /b.txt -> /c.txt.
	renameIn := fuseRenameIn{Newdir: 1}
	renameBody := c.bodyWithNames(encodeStruct(&renameIn), "b.txt", "c.txt")
	errno, _ = c.request(t, fuseOpRename, 1, renameBody)
	if errno != 0 {
		t.Fatalf("rename errno %d", errno)
	}
	if _, err := store.Stat("/c.txt"); err != nil {
		t.Fatalf("rename target: %v", err)
	}

	// SYMLINK + READLINK. The kernel's wire form is link_name\0link_target\0,
	// so the first name is the new link and the second is its target.
	symlinkBody := c.bodyWithNames(nil, "link", "target.txt")
	errno, resp = c.request(t, fuseOpSymlink, 1, symlinkBody)
	if errno != 0 {
		t.Fatalf("symlink errno %d", errno)
	}
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	errno, resp = c.request(t, fuseOpReadlink, entry.Nodeid, nil)
	if errno != 0 {
		t.Fatalf("readlink errno %d", errno)
	}
	if string(resp) != "target.txt" {
		t.Fatalf("readlink %q", resp)
	}

	// STATFS.
	errno, _ = c.request(t, fuseOpStatfs, 1, nil)
	if errno != 0 {
		t.Fatalf("statfs errno %d", errno)
	}
}

func parseDirentNames(t *testing.T, body []byte) []string {
	t.Helper()
	var names []string
	for len(body) >= 24 {
		ino := binary.LittleEndian.Uint64(body[0:8])
		off := binary.LittleEndian.Uint64(body[8:16])
		namelen := binary.LittleEndian.Uint32(body[16:20])
		if uint64(len(body)) < 24+uint64(namelen) {
			break
		}
		names = append(names, string(body[24:24+namelen]))
		_ = ino
		_ = off
		padded := (24 + int(namelen) + 7) &^ 7
		if padded > len(body) {
			break
		}
		body = body[padded:]
	}
	return names
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func TestFuseServerForgetHasNoReply(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)

	// A FORGET with unique 0 carries no reply; if the server replied, the
	// next legitimate request would see a stale response unique.
	c.unique++
	h := fuseInHeader{
		Len:    uint32(binary.Size(fuseInHeader{}) + binary.Size(fuseForgetIn{})),
		Opcode: fuseOpForget,
		Unique: 0,
		Nodeid: 1,
	}
	hdr := make([]byte, binary.Size(h))
	_ = binary.Write(bytes.NewBuffer(hdr[:0]), binary.LittleEndian, &h)
	body := encodeStruct(&fuseForgetIn{Nlookup: 1})
	if _, err := unix.Write(c.fd, append(hdr, body...)); err != nil {
		t.Fatal(err)
	}
	// A STATFS response must carry the following request's unique, not
	// the FORGET's zero.
	errno, _ := c.request(t, fuseOpStatfs, 1, nil)
	if errno != 0 {
		t.Fatalf("statfs errno %d", errno)
	}
}

func TestMountOptionsRequirePaths(t *testing.T) {
	if _, err := Mount(MountOptions{DBPath: "x.db"}); err == nil {
		t.Fatal("empty mountpoint must fail")
	}
	// The memory layer names no record, so a mountpoint alone is a valid
	// request for this API; the tai tool names a per-session directory
	// record instead. See TheoryOfUpperLayer.
	if err := validateMountOptions(MountOptions{Mountpoint: t.TempDir()}); err != nil {
		t.Fatalf("the memory layer needs no record: %v", err)
	}
}

func TestFuseServerCreateRespectsFlags(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "keep.txt", "original")
	c := newFuseClient(t, store)

	// O_CREAT without O_TRUNC keeps an existing file's content.
	createIn := fuseCreateIn{Flags: uint32(unix.O_WRONLY | unix.O_CREAT), Mode: 0o644}
	errno, _ := c.request(t, fuseOpCreate, 1, c.bodyWithNames(encodeStruct(&createIn), "keep.txt"))
	if errno != 0 {
		t.Fatalf("create errno %d", errno)
	}
	if got, err := store.ReadFile("/keep.txt"); err != nil || string(got) != "original" {
		t.Fatalf("create without O_TRUNC changed content: %q %v", got, err)
	}

	// O_CREAT|O_EXCL on an existing path reports EEXIST.
	createIn = fuseCreateIn{Flags: uint32(unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL), Mode: 0o644}
	errno, _ = c.request(t, fuseOpCreate, 1, c.bodyWithNames(encodeStruct(&createIn), "keep.txt"))
	if errno != -int32(syscall.EEXIST) {
		t.Fatalf("create O_EXCL errno %d", errno)
	}

	// O_TRUNC empties the existing file.
	createIn = fuseCreateIn{Flags: uint32(unix.O_WRONLY | unix.O_CREAT | unix.O_TRUNC), Mode: 0o644}
	errno, _ = c.request(t, fuseOpCreate, 1, c.bodyWithNames(encodeStruct(&createIn), "keep.txt"))
	if errno != 0 {
		t.Fatalf("create O_TRUNC errno %d", errno)
	}
	if got, err := store.ReadFile("/keep.txt"); err != nil || len(got) != 0 {
		t.Fatalf("create O_TRUNC left %q %v", got, err)
	}
}

func TestFuseServerMknodFifo(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)

	mknodIn := fuseMknodIn{Mode: uint32(syscall.S_IFIFO | 0o644)}
	errno, resp := c.request(t, fuseOpMknod, 1, c.bodyWithNames(encodeStruct(&mknodIn), "pipe"))
	if errno != 0 {
		t.Fatalf("mknod errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Attr.Mode&uint32(syscall.S_IFMT) != uint32(syscall.S_IFIFO) {
		t.Fatalf("mode %#o", entry.Attr.Mode)
	}
	a, err := store.Stat("/pipe")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindFIFO {
		t.Fatalf("kind %v", a.Kind)
	}
	entries, err := store.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Name == "pipe" {
			found = true
			if e.Attr.Kind != KindFIFO {
				t.Fatalf("entry kind %v", e.Attr.Kind)
			}
		}
	}
	if !found {
		t.Fatal("pipe missing from the listing")
	}
	// A device node is refused rather than recorded.
	mknodIn = fuseMknodIn{Mode: uint32(syscall.S_IFCHR | 0o644)}
	errno, _ = c.request(t, fuseOpMknod, 1, c.bodyWithNames(encodeStruct(&mknodIn), "dev"))
	if errno != -int32(syscall.EPERM) {
		t.Fatalf("device mknod errno %d", errno)
	}
	if _, err := store.Stat("/dev"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("refused node was recorded: %v", err)
	}
}

func TestFuseServerCreateOnDirectoryReportsEISDIR(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	c := newFuseClient(t, store)
	// O_CREAT without O_TRUNC takes the create-open path that does not
	// rewrite the target; a directory must be refused before the response.
	createIn := fuseCreateIn{Flags: uint32(unix.O_RDWR | unix.O_CREAT), Mode: 0o644}
	body := c.bodyWithNames(encodeStruct(&createIn), "d")
	errno, _ := c.request(t, fuseOpCreate, 1, body)
	if errno != -int32(syscall.EISDIR) {
		t.Fatalf("create on a directory: want EISDIR, got %d", errno)
	}
}

// TestFuseServerCreateKeepsReadOnlyPathOutsidePolicy protects TheoryOfWritePolicy
// and TheoryOfCompletePOSIX: a create-open that finds the path already there and
// carries no O_TRUNC mutates nothing, so the write policy has no say, exactly as
// for a plain read-only open. Only a write-mode create-open asks the policy.
func TestFuseServerCreateKeepsReadOnlyPathOutsidePolicy(t *testing.T) {
	base := t.TempDir()
	writeHost(t, base, "kept.txt", "k")
	store, err := OpenWithPolicy(filepath.Join(t.TempDir(), "sandbox.db"), base,
		func(p string) error { return fmt.Errorf("%w: %s", os.ErrPermission, p) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	c := newFuseClient(t, store)

	// A read-only create-open of an existing file passes: nothing is mutated.
	createIn := fuseCreateIn{Flags: uint32(unix.O_RDONLY | unix.O_CREAT), Mode: 0o644}
	if errno, _ := c.request(t, fuseOpCreate, fuseRootID,
		c.bodyWithNames(encodeStruct(&createIn), "kept.txt")); errno != 0 {
		t.Fatalf("read-only create-open outside policy: errno %d, want success", errno)
	}
	if got, err := store.ReadFile("/kept.txt"); err != nil || string(got) != "k" {
		t.Fatalf("the read-only create-open changed the file: %q %v", got, err)
	}

	// A write-mode create-open still reports EACCES at open time.
	createIn = fuseCreateIn{Flags: uint32(unix.O_WRONLY | unix.O_CREAT), Mode: 0o644}
	if errno, _ := c.request(t, fuseOpCreate, fuseRootID,
		c.bodyWithNames(encodeStruct(&createIn), "kept.txt")); errno != -int32(syscall.EACCES) {
		t.Fatalf("write-mode create-open outside policy: want EACCES, got %d", errno)
	}
}

func TestFuseServerUnlinkRespectsType(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "d/a.txt", "x")
	c := newFuseClient(t, store)

	// UNLINK of a directory reports EISDIR, so a caller's unlink-or-rmdir
	// fallback follows the same path a local filesystem would.
	errno, _ := c.request(t, fuseOpUnlink, 1, c.bodyWithNames(nil, "d"))
	if errno != -int32(syscall.EISDIR) {
		t.Fatalf("unlink of a directory: want EISDIR, got %d", errno)
	}

	// RMDIR of a non-empty directory reports ENOTEMPTY.
	errno, _ = c.request(t, fuseOpRmdir, 1, c.bodyWithNames(nil, "d"))
	if errno != -int32(syscall.ENOTEMPTY) {
		t.Fatalf("rmdir non-empty directory: want ENOTEMPTY, got %d", errno)
	}

	// A regular file: rmdir reports ENOTDIR, unlink succeeds.
	if err := store.WriteFile("/f.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	errno, _ = c.request(t, fuseOpRmdir, 1, c.bodyWithNames(nil, "f.txt"))
	if errno != -int32(syscall.ENOTDIR) {
		t.Fatalf("rmdir of a regular file: want ENOTDIR, got %d", errno)
	}
	errno, _ = c.request(t, fuseOpUnlink, 1, c.bodyWithNames(nil, "f.txt"))
	if errno != 0 {
		t.Fatalf("unlink regular file errno %d", errno)
	}

	// RMDIR of a file's directory entry leaves the entry alone; UNLINK of
	// the file followed by RMDIR of the now-empty directory succeeds. A name
	// is one entry of the parent the request names, so the directory comes
	// from a LOOKUP first: the kernel never puts a separator in a name.
	// See TheoryOfFuseMount.
	errno, resp := c.request(t, fuseOpLookup, fuseRootID, c.bodyWithNames(nil, "d"))
	if errno != 0 {
		t.Fatalf("lookup of /d: errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	errno, _ = c.request(t, fuseOpUnlink, entry.Nodeid, c.bodyWithNames(nil, "a.txt"))
	if errno != 0 {
		t.Fatalf("unlink of a.txt from /d: errno %d", errno)
	}
	errno, _ = c.request(t, fuseOpRmdir, fuseRootID, c.bodyWithNames(nil, "d"))
	if errno != 0 {
		t.Fatalf("rmdir of an empty directory errno %d", errno)
	}
}

func TestFuseServerSymlinkWireOrder(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)

	// Kernel FUSE_SYMLINK form: link_name\0link_target\0.
	body := c.bodyWithNames(nil, "mylink", "target.txt")
	errno, resp := c.request(t, fuseOpSymlink, 1, body)
	if errno != 0 {
		t.Fatalf("symlink errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	// The link lives at the first path; the target string is what a
	// readlink returns.
	got, err := store.Readlink("/mylink")
	if err != nil {
		t.Fatalf("link missing at its own path: %v", err)
	}
	if got != "target.txt" {
		t.Fatalf("link target %q, want %q", got, "target.txt")
	}
	if _, err := store.Stat("/target.txt"); err == nil {
		t.Fatal("symlink created a node at the target path")
	}
}

// TestReaddirRefreshesAfterAMountMutation protects TheoryOfReadPath: the
// kernel cannot see a change this mount makes, so a read that follows a
// mutation through the same directory handle would report the state before
// the session's own entry unless the server clears the snapshot its handler
// mutated. The test drives the mutation through the wire, because that is the
// path the server serves.
func TestReaddirRefreshesAfterAMountMutation(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.WriteFile("/a", []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newFuseClient(t, store)
	errno, resp := c.request(t, fuseOpOpendir, fuseRootID, encodeStruct(&fuseOpenIn{}))
	if errno != 0 {
		t.Fatalf("opendir errno %d", errno)
	}
	var openOut fuseOpenOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &openOut); err != nil {
		t.Fatal(err)
	}
	readAll := func() []string {
		t.Helper()
		errno, resp := c.request(t, fuseOpReaddir, fuseRootID, encodeStruct(&fuseReadIn{
			Fh: openOut.Fh, Offset: 0, Size: 4096,
		}))
		if errno != 0 {
			t.Fatalf("readdir errno %d", errno)
		}
		return parseDirentNames(t, resp)
	}
	if names := readAll(); !contains(names, "a") {
		t.Fatalf("first listing %v, want a", names)
	}
	createIn := fuseCreateIn{Flags: uint32(unix.O_WRONLY | unix.O_CREAT), Mode: 0o644}
	if errno, _ := c.request(t, fuseOpCreate, fuseRootID,
		c.bodyWithNames(encodeStruct(&createIn), "b")); errno != 0 {
		t.Fatalf("create errno %d", errno)
	}
	if names := readAll(); !contains(names, "b") {
		t.Fatalf("listing after the create %v, want b", names)
	}
	if errno, _ := c.request(t, fuseOpUnlink, fuseRootID, c.bodyWithNames(nil, "a")); errno != 0 {
		t.Fatalf("unlink errno %d", errno)
	}
	if names := readAll(); contains(names, "a") {
		t.Fatalf("listing after the delete %v, must not hold a", names)
	}
}

// TestFuseServerAccessFollowsThePolicy protects TheoryOfWritePolicy: an access
// probe answers W_OK from the write policy, so a caller that asks whether it may
// write learns the same answer its first write would give. A read-only probe
// stays admitted, and the reply body stays empty as the kernel requires.
func TestFuseServerAccessFollowsThePolicy(t *testing.T) {
	base := t.TempDir()
	writeHost(t, base, "f", "x")
	store, err := OpenWithPolicy(filepath.Join(t.TempDir(), "sandbox.db"), base,
		func(p string) error { return fmt.Errorf("%w: %s", os.ErrPermission, p) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	c := newFuseClient(t, store)
	errno, resp := c.request(t, fuseOpLookup, fuseRootID, c.bodyWithNames(nil, "f"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	const (
		wOK = 2
		rOK = 4
	)
	if errno, resp := c.request(t, fuseOpAccess, entry.Nodeid, encodeStruct(&fuseAccessIn{Mask: rOK})); errno != 0 || len(resp) != 0 {
		t.Fatalf("read-only access: errno %d, body %d bytes, want success with an empty body", errno, len(resp))
	}
	if errno, _ := c.request(t, fuseOpAccess, entry.Nodeid, encodeStruct(&fuseAccessIn{Mask: wOK})); errno != -int32(syscall.EACCES) {
		t.Fatalf("write access outside policy: want EACCES, got %d", errno)
	}
}

// TestSymlinkAttributeReportsMode0777 protects TheoryOfCompletePOSIX: Linux
// reports every symbolic link with mode 0o777, because a link's permission
// bits are not its own. The store's row for a link the session put where a
// base file stood carries that file's bits, and the reply must not turn
// lrwxrwxrwx into lrw-r--r--.
func TestSymlinkAttributeReportsMode0777(t *testing.T) {
	store, base := newTestStore(t)
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(base, "f"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The session replaces the base file with a symbolic link, so the row's
	// mode is the base file's own.
	if err := store.Remove("/f"); err != nil {
		t.Fatal(err)
	}
	if err := store.Symlink("target", "/f"); err != nil {
		t.Fatal(err)
	}
	a, err := store.Stat("/f")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindSymlink {
		t.Fatalf("kind %v, want a symbolic link", a.Kind)
	}
	want := uint32(syscall.S_IFLNK | 0o777)
	if got := attrOfFuse(1, a).Mode; got != want {
		t.Errorf("symlink mode %#o, want %#o", got, want)
	}
	// A regular file still carries the mode its row recorded.
	if err := store.WriteFile("/g", []byte("y"), 0o640); err != nil {
		t.Fatal(err)
	}
	if a, err := store.Stat("/g"); err != nil {
		t.Fatal(err)
	} else if got := attrOfFuse(1, a).Mode; got != uint32(syscall.S_IFREG|0o640) {
		t.Errorf("regular mode %#o, want %#o", got, uint32(syscall.S_IFREG|0o640))
	}
}

func TestFuseServerRenameRespectsNoReplace(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "a.txt", "a")
	writeHost(t, base, "b.txt", "b")
	c := newFuseClient(t, store)

	// RENAME_NOREPLACE refuses an existing destination.
	renameIn := fuseRename2In{Newdir: 1, Flags: 1}
	body := c.bodyWithNames(encodeStruct(&renameIn), "a.txt", "b.txt")
	errno, _ := c.request(t, fuseOpRename2, 1, body)
	if errno != -int32(syscall.EEXIST) {
		t.Fatalf("rename_noreplace over existing: want EEXIST, got %d", errno)
	}
	if got, err := store.ReadFile("/b.txt"); err != nil || string(got) != "b" {
		t.Fatalf("rename_noreplace overwrote destination: %q %v", got, err)
	}

	// RENAME_NOREPLACE succeeds when the destination is absent.
	renameIn = fuseRename2In{Newdir: 1, Flags: 1}
	body = c.bodyWithNames(encodeStruct(&renameIn), "a.txt", "c.txt")
	errno, _ = c.request(t, fuseOpRename2, 1, body)
	if errno != 0 {
		t.Fatalf("rename_noreplace absent destination: errno %d", errno)
	}
	if got, err := store.ReadFile("/c.txt"); err != nil || string(got) != "a" {
		t.Fatalf("rename_noreplace did not move: %q %v", got, err)
	}

	// RENAME_EXCHANGE is refused with ENOSYS so a caller falls back.
	renameIn = fuseRename2In{Newdir: 1, Flags: 2}
	body = c.bodyWithNames(encodeStruct(&renameIn), "b.txt", "c.txt")
	errno, _ = c.request(t, fuseOpRename2, 1, body)
	if errno != -int32(syscall.ENOSYS) {
		t.Fatalf("rename_exchange: want ENOSYS, got %d", errno)
	}
}

// TestFuseServerRenameNoReplaceReportsMissingSource protects
// TheoryOfCompletePOSIX: renameat2 with RENAME_NOREPLACE resolves the source
// before it tests the flag, so a missing source reports ENOENT whatever the
// destination holds. Testing the destination first reports EEXIST, and a
// caller that reads EEXIST as another writer's claim gives up on a rename its
// own missing source caused.
func TestFuseServerRenameNoReplaceReportsMissingSource(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "b.txt", "b")
	c := newFuseClient(t, store)

	renameIn := fuseRename2In{Newdir: 1, Flags: 1}
	body := c.bodyWithNames(encodeStruct(&renameIn), "missing.txt", "b.txt")
	errno, _ := c.request(t, fuseOpRename2, 1, body)
	if errno != -int32(syscall.ENOENT) {
		t.Fatalf("rename_noreplace with a missing source: got %d, want ENOENT", errno)
	}
	if got, err := store.ReadFile("/b.txt"); err != nil || string(got) != "b" {
		t.Fatalf("the destination changed on the refused rename: %q %v", got, err)
	}
}

func TestFuseServerFsyncdirSucceeds(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)
	if err := store.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	errno, resp := c.request(t, fuseOpLookup, 1, c.bodyWithNames(nil, "d"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	// OPEN of a directory is refused: a directory is served by OPENDIR and
	// READDIR, not by the regular-file READ/WRITE pair.
	if errno, _ := c.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{})); errno != -int32(syscall.EISDIR) {
		t.Fatalf("open dir must fail with EISDIR, got %d", errno)
	}
	// FSYNCDIR succeeds: the store's writes persist at every mutation, so
	// the durability guarantee is already satisfied.
	if errno, _ := c.request(t, fuseOpFsyncdir, entry.Nodeid, encodeStruct(&fuseFsyncIn{})); errno != 0 {
		t.Fatalf("fsyncdir: want success, got %d", errno)
	}
}

// TestFsyncdirUsesTheKernelOpcode protects TheoryOfCompletePOSIX: FSYNC and
// FSYNCDIR succeed without doing anything, and the request the kernel sends for
// a directory sync is FUSE_FSYNCDIR, opcode 30. A table that names another
// number leaves the kernel's directory sync unanswered — it reports ENOSYS and
// logs the gap once — and answers whatever request does carry the declared
// number, which here is FUSE_SETLK, a byte-range lock this store refuses.
func TestFsyncdirUsesTheKernelOpcode(t *testing.T) {
	if fuseOpFsyncdir != 30 {
		t.Fatalf("FUSE_FSYNCDIR is opcode 30; the server declares %d", fuseOpFsyncdir)
	}
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)
	body := encodeStruct(&fuseFsyncIn{})
	if errno, _ := c.request(t, 30, uint64(fuseRootID), body); errno != 0 {
		t.Errorf("FSYNCDIR at the kernel's opcode: errno %d, want success", errno)
	}
	if errno, _ := c.request(t, 32, uint64(fuseRootID), body); errno != -int32(syscall.ENOSYS) {
		t.Errorf("opcode 32 is FUSE_SETLK, which the store does not implement: errno %d, want ENOSYS", errno)
	}
}

func TestFuseServerOpenTruncates(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "a.txt", "original")
	c := newFuseClient(t, store)

	errno, resp := c.request(t, fuseOpLookup, 1, c.bodyWithNames(nil, "a.txt"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}

	// A plain OPEN without O_TRUNC keeps the content.
	errno, _ = c.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{
		Flags: uint32(unix.O_RDONLY),
	}))
	if errno != 0 {
		t.Fatalf("open errno %d", errno)
	}
	if got, err := store.ReadFile("/a.txt"); err != nil || string(got) != "original" {
		t.Fatalf("read-only open changed content: %q %v", got, err)
	}

	// OPEN with O_WRONLY|O_TRUNC empties the file, as POSIX requires.
	errno, resp = c.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{
		Flags: uint32(unix.O_WRONLY | unix.O_TRUNC),
	}))
	if errno != 0 {
		t.Fatalf("open O_TRUNC errno %d", errno)
	}
	var openOut fuseOpenOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &openOut); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ReadFile("/a.txt"); err != nil || len(got) != 0 {
		t.Fatalf("open O_TRUNC left %q %v", got, err)
	}

	// Writing the truncated handle extends from zero.
	payload := []byte("new")
	writeIn := fuseWriteIn{Fh: openOut.Fh, Offset: 0, Size: uint32(len(payload))}
	errno, _ = c.request(t, fuseOpWrite, entry.Nodeid, append(encodeStruct(&writeIn), payload...))
	if errno != 0 {
		t.Fatalf("write errno %d", errno)
	}
	if got, err := store.ReadFile("/a.txt"); err != nil || string(got) != "new" {
		t.Fatalf("post-truncate write: %q %v", got, err)
	}
}

// TestFuseServerOpenTruncatesReadOnly protects TheoryOfCompletePOSIX and
// TheoryOfOpenCaching: a truncating open empties the file whatever the access
// mode, because POSIX gives O_TRUNC the same effect on a read-only open as on a
// write-mode one. The truncation is a mutation, so it still passes the write
// policy: a path the policy refuses reports EACCES at open instead of being
// emptied. And the reply must not ask the kernel to keep the pages it held
// before, because the content the request just stored would be shadowed.
func TestFuseServerOpenTruncatesReadOnly(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "a.txt", "original")
	c := newFuseClient(t, store)
	errno, resp := c.request(t, fuseOpLookup, 1, c.bodyWithNames(nil, "a.txt"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	errno, resp = c.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{
		Flags: uint32(unix.O_RDONLY | unix.O_TRUNC),
	}))
	if errno != 0 {
		t.Fatalf("read-only open O_TRUNC errno %d", errno)
	}
	if got, err := store.ReadFile("/a.txt"); err != nil || len(got) != 0 {
		t.Fatalf("read-only open O_TRUNC left %q %v", got, err)
	}
	var openOut fuseOpenOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &openOut); err != nil {
		t.Fatal(err)
	}
	if openOut.OpenFlags&fuseOpenKeepCache != 0 {
		t.Errorf("a truncating open asks the kernel to keep the page cache (%#x)", openOut.OpenFlags)
	}

	// The same open outside the write policy is refused at open time, and the
	// host content stays.
	refused, err := OpenWithPolicy(filepath.Join(t.TempDir(), "sandbox.db"), base,
		func(p string) error { return fmt.Errorf("%w: %s", os.ErrPermission, p) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = refused.Close() })
	writeHost(t, base, "b.txt", "keep")
	rc := newFuseClient(t, refused)
	errno, resp = rc.request(t, fuseOpLookup, 1, rc.bodyWithNames(nil, "b.txt"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	if errno, _ := rc.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{
		Flags: uint32(unix.O_RDONLY | unix.O_TRUNC),
	})); errno != -int32(syscall.EACCES) {
		t.Fatalf("read-only open O_TRUNC outside policy: want EACCES, got %d", errno)
	}
	if got, err := refused.ReadFile("/b.txt"); err != nil || string(got) != "keep" {
		t.Errorf("the refused open emptied the file: %q %v", got, err)
	}
}

// TestFuseServerBoundsTheSizeField protects TheoryOfCompletePOSIX: a request's
// size is bounded before it becomes a length or an allocation. A write whose
// size, offset, or offset-plus-size passes the store's bound reports EFBIG, so
// a malformed claim never slices a body or reserves memory. The order matters:
// on a 32-bit build a size above 2^31 turns into a negative int, and a length
// check that ran first would pass on a body the slice cannot hold. A read names
// a handle first — a bad one is EBADF, as Linux answers — so the bound is
// checked on an open handle.
func TestFuseServerBoundsTheSizeField(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "f", "x")
	c := newFuseClient(t, store)
	errno, resp := c.request(t, fuseOpLookup, fuseRootID, c.bodyWithNames(nil, "f"))
	if errno != 0 {
		t.Fatalf("lookup of /f: errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	errno, resp = c.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{
		Flags: uint32(unix.O_RDONLY),
	}))
	if errno != 0 {
		t.Fatalf("open of /f: errno %d", errno)
	}
	var openOut fuseOpenOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &openOut); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		opcode uint32
		body   []byte
	}{
		{"write claims more than the bound", fuseOpWrite, encodeStruct(&fuseWriteIn{
			Fh: openOut.Fh, Offset: 0, Size: uint32(maxFileSize) + 1,
		})},
		{"write past the bound", fuseOpWrite, encodeStruct(&fuseWriteIn{
			Fh: openOut.Fh, Offset: uint64(maxFileSize) + 1, Size: 1,
		})},
		{"read claims more than the bound", fuseOpRead, encodeStruct(&fuseReadIn{
			Fh: openOut.Fh, Offset: 0, Size: uint32(maxFileSize) + 1,
		})},
	}
	for _, tc := range cases {
		errno, _ := c.request(t, tc.opcode, entry.Nodeid, tc.body)
		if errno != -int32(syscall.EFBIG) {
			t.Errorf("%s: errno %d, want EFBIG", tc.name, errno)
		}
	}
	// A write at the highest legal offset still lands.
	if err := store.WriteFile("/g", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	errno, resp = c.request(t, fuseOpLookup, fuseRootID, c.bodyWithNames(nil, "g"))
	if errno != 0 {
		t.Fatalf("lookup of /g: errno %d", errno)
	}
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	errno, resp = c.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{
		Flags: uint32(unix.O_WRONLY),
	}))
	if errno != 0 {
		t.Fatalf("open of /g: errno %d", errno)
	}
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &openOut); err != nil {
		t.Fatal(err)
	}
	payload := []byte("x")
	writeIn := fuseWriteIn{
		Fh: openOut.Fh, Offset: uint64(maxFileSize) - 1, Size: uint32(len(payload)),
	}
	if errno, _ := c.request(t, fuseOpWrite, entry.Nodeid,
		append(encodeStruct(&writeIn), payload...)); errno != 0 {
		t.Fatalf("write at the highest legal offset: errno %d", errno)
	}
	a, err := store.Stat("/g")
	if err != nil {
		t.Fatal(err)
	}
	if a.Size != maxFileSize {
		t.Errorf("size after the highest legal write: got %d, want %d", a.Size, maxFileSize)
	}
}

// TestFuseServerAppliesTheUmask protects TheoryOfCompletePOSIX: a create,
// mkdir, or mknod request carries the caller's umask beside its mode, and the
// kernel hands both so the filesystem applies the mask, as every local
// filesystem does. Ignoring the umask records a file created as 0666 under
// umask 022 with bits the process withdrew, and the apply then widens the
// host's permissions the same way.
func TestFuseServerAppliesTheUmask(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)

	createIn := fuseCreateIn{
		Flags: uint32(unix.O_WRONLY | unix.O_CREAT), Mode: 0o666, Umask: 0o022,
	}
	if errno, _ := c.request(t, fuseOpCreate, fuseRootID,
		c.bodyWithNames(encodeStruct(&createIn), "f")); errno != 0 {
		t.Fatalf("create errno %d", errno)
	}
	if a, err := store.Stat("/f"); err != nil || a.Mode != 0o644 {
		t.Errorf("create with umask 022: got mode %o (error %v), want 644", a.Mode, err)
	}

	mkdirIn := fuseMkdirIn{Mode: 0o777, Umask: 0o027}
	if errno, _ := c.request(t, fuseOpMkdir, fuseRootID,
		c.bodyWithNames(encodeStruct(&mkdirIn), "d")); errno != 0 {
		t.Fatalf("mkdir errno %d", errno)
	}
	if a, err := store.Stat("/d"); err != nil || a.Mode != 0o750 {
		t.Errorf("mkdir with umask 027: got mode %o (error %v), want 750", a.Mode, err)
	}

	mknodIn := fuseMknodIn{Mode: uint32(syscall.S_IFIFO | 0o666), Umask: 0o022}
	if errno, _ := c.request(t, fuseOpMknod, fuseRootID,
		c.bodyWithNames(encodeStruct(&mknodIn), "p")); errno != 0 {
		t.Fatalf("mknod errno %d", errno)
	}
	if a, err := store.Stat("/p"); err != nil || a.Mode != 0o644 {
		t.Errorf("mknod with umask 022: got mode %o (error %v), want 644", a.Mode, err)
	}

	// A mode of no bits stays a choice below any mask.
	createIn = fuseCreateIn{
		Flags: uint32(unix.O_WRONLY | unix.O_CREAT), Mode: 0, Umask: 0o022,
	}
	if errno, _ := c.request(t, fuseOpCreate, fuseRootID,
		c.bodyWithNames(encodeStruct(&createIn), "z")); errno != 0 {
		t.Fatalf("create with mode 0 errno %d", errno)
	}
	if a, err := store.Stat("/z"); err != nil || a.Mode != 0 {
		t.Errorf("create with mode 0 and umask 022: got mode %o (error %v), want 000", a.Mode, err)
	}
}

func TestFuseServerOpenRefusesWriteOutsidePolicy(t *testing.T) {
	base := t.TempDir()
	allow := func(p string) error {
		if p == "/allowed.txt" {
			return nil
		}
		return fmt.Errorf("%w: %s", os.ErrPermission, p)
	}
	s, err := OpenWithPolicy(filepath.Join(t.TempDir(), "sandbox.db"), base, allow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	writeHost(t, base, "allowed.txt", "a")
	writeHost(t, base, "denied.txt", "d")

	c := newFuseClient(t, s)
	errno, resp := c.request(t, fuseOpLookup, 1, c.bodyWithNames(nil, "denied.txt"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	// A read-only open is allowed regardless of the write policy.
	if errno, _ := c.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{
		Flags: uint32(unix.O_RDONLY),
	})); errno != 0 {
		t.Fatalf("read-only open must succeed: %d", errno)
	}
	// A write-mode open reports EACCES at open time.
	if errno, _ := c.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{
		Flags: uint32(unix.O_WRONLY),
	})); errno != -int32(syscall.EACCES) {
		t.Fatalf("write-mode open outside policy: want EACCES, got %d", errno)
	}
}

// TestOpenReplyKeepsThePageCache protects TheoryOfOpenCaching: the open-reply
// bit that asks the kernel to keep a file's pages across opens is
// FOPEN_KEEP_CACHE, bit 1 of the open flags. Bit 0 is FOPEN_DIRECT_IO, which
// does the opposite: it bypasses the page cache, disables readahead, and makes
// mmap of the file fail, so a read-only open that sets it turns every repeated
// read into a request and breaks a toolchain that maps its input.
func TestOpenReplyKeepsThePageCache(t *testing.T) {
	const (
		fopenDirectIO  = 1 << 0
		fopenKeepCache = 1 << 1
	)
	if fuseOpenKeepCache != fopenKeepCache {
		t.Fatalf("FOPEN_KEEP_CACHE is bit 1; the server declares %#x", fuseOpenKeepCache)
	}
	store, base := newTestStore(t)
	writeHost(t, base, "a.txt", "hello")
	s := newFuseServer(store, -1)
	id, err := s.nodeOf("/a.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.handleOpen(id, encodeStruct(&fuseOpenIn{Flags: uint32(unix.O_RDONLY)}))
	if err != nil {
		t.Fatal(err)
	}
	var out fuseOpenOut
	decodeStruct(&out, reply)
	if out.OpenFlags&fopenDirectIO != 0 {
		t.Errorf("a read-only open asks for direct I/O (%#x): the page cache is bypassed", out.OpenFlags)
	}
	if out.OpenFlags&fopenKeepCache == 0 {
		t.Errorf("a read-only open does not ask to keep the page cache (%#x)", out.OpenFlags)
	}
}

func TestFuseServerMultiHandleWritePreserved(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)

	createIn := fuseCreateIn{Flags: uint32(unix.O_RDWR | unix.O_CREAT | unix.O_TRUNC), Mode: 0o644}
	errno, resp := c.request(t, fuseOpCreate, 1, c.bodyWithNames(encodeStruct(&createIn), "shared.txt"))
	if errno != 0 {
		t.Fatalf("create errno %d", errno)
	}
	var createOut fuseCreateOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &createOut); err != nil {
		t.Fatal(err)
	}
	first := createOut.Open.Fh
	aNode := createOut.Entry.Nodeid

	// A second handle to the same path.
	errno, resp = c.request(t, fuseOpOpen, aNode, encodeStruct(&fuseOpenIn{
		Flags: uint32(unix.O_RDWR),
	}))
	if errno != 0 {
		t.Fatalf("open errno %d", errno)
	}
	var openOut fuseOpenOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &openOut); err != nil {
		t.Fatal(err)
	}
	second := openOut.Fh

	// Write "AAA" through the first handle at offset 0.
	payload := []byte("AAA")
	w := fuseWriteIn{Fh: first, Offset: 0, Size: uint32(len(payload))}
	errno, _ = c.request(t, fuseOpWrite, aNode, append(encodeStruct(&w), payload...))
	if errno != 0 {
		t.Fatalf("write first errno %d", errno)
	}

	// Write "BB" through the second handle at offset 3. The store's current
	// content is "AAA", so the second write must extend it, not overwrite
	// the first handle's bytes with a stale empty buffer.
	payload = []byte("BB")
	w = fuseWriteIn{Fh: second, Offset: 3, Size: uint32(len(payload))}
	errno, _ = c.request(t, fuseOpWrite, aNode, append(encodeStruct(&w), payload...))
	if errno != 0 {
		t.Fatalf("write second errno %d", errno)
	}
	got, err := store.ReadFile("/shared.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "AAABB" {
		t.Fatalf("multi-handle content %q, want %q", got, "AAABB")
	}
}

func TestFuseServerReadStreamsWindow(t *testing.T) {
	store, _ := newTestStore(t)
	content := bytes.Repeat([]byte("0123456789"), 200_000)
	if err := store.WriteFile("/big.bin", content, 0o644); err != nil {
		t.Fatal(err)
	}
	c := newFuseClient(t, store)
	errno, resp := c.request(t, fuseOpLookup, 1, c.bodyWithNames(nil, "big.bin"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	errno, resp = c.request(t, fuseOpOpen, entry.Nodeid, encodeStruct(&fuseOpenIn{}))
	if errno != 0 {
		t.Fatalf("open errno %d", errno)
	}
	var openOut fuseOpenOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &openOut); err != nil {
		t.Fatal(err)
	}
	off := 1_000_000
	want := content[off : off+17]
	errno, resp = c.request(t, fuseOpRead, entry.Nodeid, encodeStruct(&fuseReadIn{
		Fh: openOut.Fh, Offset: uint64(off), Size: 17,
	}))
	if errno != 0 {
		t.Fatalf("read errno %d", errno)
	}
	if !bytes.Equal(resp, want) {
		t.Fatalf("read %q want %q", resp, want)
	}
}

func TestFuseServerSetattrUpdatesMtime(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "a.txt", "x")
	c := newFuseClient(t, store)
	errno, resp := c.request(t, fuseOpLookup, 1, c.bodyWithNames(nil, "a.txt"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	want := time.Unix(1_000_000_000, 0)
	errno, _ = c.request(t, fuseOpSetattr, entry.Nodeid, encodeStruct(&fuseSetattrIn{
		Valid: fattrMtime, Mtime: uint64(want.Unix()),
	}))
	if errno != 0 {
		t.Fatalf("setattr errno %d", errno)
	}
	a, err := store.Stat("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !a.ModTime.Equal(want) {
		t.Fatalf("mtime %v want %v", a.ModTime, want)
	}
	if got, err := store.ReadFile("/a.txt"); err != nil || string(got) != "x" {
		t.Fatalf("time-only change altered content: %q %v", got, err)
	}
}

func TestFuseServerRequestBufferHoldsMaxWrite(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)
	createIn := fuseCreateIn{Flags: uint32(unix.O_WRONLY | unix.O_CREAT | unix.O_TRUNC), Mode: 0o644}
	errno, resp := c.request(t, fuseOpCreate, 1, c.bodyWithNames(encodeStruct(&createIn), "blob.bin"))
	if errno != 0 {
		t.Fatalf("create errno %d", errno)
	}
	var createOut fuseCreateOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &createOut); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("x"), fuseMaxWrite)
	writeIn := fuseWriteIn{Fh: createOut.Open.Fh, Offset: 0, Size: uint32(len(payload))}
	writeBody := append(encodeStruct(&writeIn), payload...)
	errno, _ = c.request(t, fuseOpWrite, createOut.Entry.Nodeid, writeBody)
	if errno != 0 {
		t.Fatalf("max write errno %d", errno)
	}
	got, err := store.ReadFile("/blob.bin")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != fuseMaxWrite {
		t.Fatalf("stored %d bytes, want %d", len(got), fuseMaxWrite)
	}
}
