//go:build linux

package sandboxfs

import (
	"bytes"
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

// requestBytes builds one raw FUSE request: the header with its own length,
// the opcode, the unique tag, the node id, and the body.
func requestBytes(unique uint64, opcode uint32, nodeid uint64, body []byte) []byte {
	msg := make([]byte, fuseInHeaderSize+len(body))
	binary.LittleEndian.PutUint32(msg[0:], uint32(len(msg)))
	binary.LittleEndian.PutUint32(msg[4:], opcode)
	binary.LittleEndian.PutUint64(msg[8:], unique)
	binary.LittleEndian.PutUint64(msg[16:], nodeid)
	copy(msg[fuseInHeaderSize:], body)
	return msg
}

// readReply reads one response header and returns its errno and unique tag.
func readReply(t *testing.T, fd int) (int32, uint64) {
	t.Helper()
	buf := make([]byte, 4096)
	n, err := unix.Read(fd, buf)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if n < fuseOutHeaderSize {
		t.Fatalf("reply holds %d bytes, want at least %d", n, fuseOutHeaderSize)
	}
	return int32(binary.LittleEndian.Uint32(buf[4:])), binary.LittleEndian.Uint64(buf[8:])
}

// TestDispatchRejectsImpossibleLength protects TheoryOfFuseMount: the request
// header's length is checked before the body is sliced. The read buffer is
// reused, so a length that claims bytes the read did not deliver would hand
// the handler the previous request's bytes; the reply is EINVAL instead.
func TestDispatchRejectsImpossibleLength(t *testing.T) {
	store, _ := newTestStore(t)
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET, 0)
	if err != nil {
		t.Fatal(err)
	}
	s := newFuseServer(store, fds[0])
	t.Cleanup(func() {
		_ = unix.Close(fds[0])
		_ = unix.Close(fds[1])
	})
	cases := []struct {
		name string
		Len  uint32
		want int32
	}{
		{"length past the read", uint32(fuseInHeaderSize + 100), -int32(unix.EINVAL)},
		{"length below the header", 4, -int32(unix.EINVAL)},
		{"length equal to the read", uint32(fuseInHeaderSize), 0},
	}
	for _, tc := range cases {
		msg := requestBytes(7, fuseOpGetattr, fuseRootID, nil)
		binary.LittleEndian.PutUint32(msg[0:], tc.Len)
		s.dispatch(msg)
		errno, unique := readReply(t, fds[1])
		if unique != 7 {
			t.Fatalf("%s: reply tag %d, want 7", tc.name, unique)
		}
		if errno != tc.want {
			t.Errorf("%s: errno %d, want %d", tc.name, errno, tc.want)
		}
	}
}

// TestFuseServerMalformedBodyReportsEINVAL protects TheoryOfFuseMount: a body
// shorter than the opcode's fixed header, or a name with no terminator, is
// malformed. The handler reports EINVAL rather than slicing past the body,
// because a panic on the server's single goroutine kills the daemon for every
// later request. The fixed header is checked before the node id it carries is
// resolved, so a short body never reports the ENOENT a zero node id earns.
func TestFuseServerMalformedBodyReportsEINVAL(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)
	cases := []struct {
		name   string
		opcode uint32
		body   []byte
	}{
		{"create with a short header", fuseOpCreate, []byte("x")},
		{"mkdir with a short header", fuseOpMkdir, []byte("x")},
		{"mknod with a short header", fuseOpMknod, []byte("x")},
		{"link with a short header", fuseOpLink, []byte("x")},
		{"rename with a short header", fuseOpRename, []byte("x")},
		{"rename2 with a short header", fuseOpRename2, []byte("x")},
		{"symlink with no name terminator", fuseOpSymlink, []byte("link")},
		{"symlink with an empty target", fuseOpSymlink, []byte("link\x00")},
		{"rename with an empty name", fuseOpRename, encodeStruct(&fuseRenameIn{Newdir: fuseRootID})},
	}
	for _, tc := range cases {
		errno, _ := c.request(t, tc.opcode, fuseRootID, tc.body)
		if errno != -int32(unix.EINVAL) {
			t.Errorf("%s: errno %d, want EINVAL", tc.name, errno)
		}
	}
	// A LOOKUP of an empty body names nothing, so the answer is ENOENT and
	// the server keeps running.
	if errno, _ := c.request(t, fuseOpLookup, fuseRootID, nil); errno != -int32(unix.ENOENT) {
		t.Errorf("lookup of an empty body: errno %d, want ENOENT", errno)
	}
	// The server survived every malformed request: a well-formed one still
	// answers.
	if errno, _ := c.request(t, fuseOpStatfs, fuseRootID, nil); errno != 0 {
		t.Errorf("statfs after the malformed requests: errno %d, want 0", errno)
	}
}

// TestFuseServerSymlinkWithAnEmptyTargetReportsENOENT protects
// TheoryOfCompletePOSIX: a body whose name and target are both terminated but
// whose target is empty is not malformed — it names no target, which POSIX
// refuses with ENOENT through the store. A body with no target field at all
// stays malformed and reports EINVAL.
func TestFuseServerSymlinkWithAnEmptyTargetReportsENOENT(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)
	// link name, NUL, empty target, NUL.
	if errno, _ := c.request(t, fuseOpSymlink, fuseRootID, []byte("empty\x00\x00")); errno != -int32(unix.ENOENT) {
		t.Errorf("symlink with an empty target: errno %d, want ENOENT", errno)
	}
	if _, err := store.Stat("/empty"); err == nil {
		t.Error("the refused link left a node")
	}
	// No target field: the body ends after the name's terminator.
	if errno, _ := c.request(t, fuseOpSymlink, fuseRootID, []byte("empty\x00")); errno != -int32(unix.EINVAL) {
		t.Errorf("symlink with no target field: errno %d, want EINVAL", errno)
	}
}

// TestFuseServerRejectsUnterminatedNames protects TheoryOfFuseMount: a name
// with no terminator is malformed, so every handler that reads a name reports
// EINVAL instead of acting on the bytes it happened to find. An empty LOOKUP
// body names nothing at all and stays ENOENT.
func TestFuseServerRejectsUnterminatedNames(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)
	cases := []struct {
		name   string
		opcode uint32
		body   []byte
	}{
		{"lookup", fuseOpLookup, []byte("a.txt")},
		{"unlink", fuseOpUnlink, []byte("a.txt")},
		{"rmdir", fuseOpRmdir, []byte("d")},
		{"create", fuseOpCreate, append(encodeStruct(&fuseCreateIn{}), []byte("a.txt")...)},
		{"mkdir", fuseOpMkdir, append(encodeStruct(&fuseMkdirIn{}), []byte("d")...)},
		{"mknod", fuseOpMknod, append(encodeStruct(&fuseMknodIn{Mode: uint32(unix.S_IFIFO)}), []byte("p")...)},
		{"link", fuseOpLink, append(encodeStruct(&fuseLinkIn{Oldnodeid: fuseRootID}), []byte("a.txt")...)},
		{"rename destination", fuseOpRename, append(encodeStruct(&fuseRenameIn{Newdir: fuseRootID}), []byte("a.txt\x00b.txt")...)},
		{"symlink target", fuseOpSymlink, []byte("link\x00target")},
	}
	for _, tc := range cases {
		errno, _ := c.request(t, tc.opcode, fuseRootID, tc.body)
		if errno != -int32(unix.EINVAL) {
			t.Errorf("%s: errno %d, want EINVAL", tc.name, errno)
		}
	}
	// Nothing a malformed request named reached the store.
	for _, p := range []string{"/a.txt", "/d", "/p", "/link", "/b.txt"} {
		if _, err := store.Stat(p); err == nil {
			t.Errorf("a malformed request created %s", p)
		}
	}
	// A terminated name still answers: the check rejects the malformed body,
	// not the opcode.
	if errno, _ := c.request(t, fuseOpCreate, fuseRootID,
		c.bodyWithNames(encodeStruct(&fuseCreateIn{Flags: uint32(unix.O_WRONLY | unix.O_CREAT)}), "ok.txt")); errno != 0 {
		t.Errorf("create with a terminated name: errno %d, want success", errno)
	}
	if _, err := store.Stat("/ok.txt"); err != nil {
		t.Errorf("the terminated create did not reach the store: %v", err)
	}
}

// TestFuseServerRejectsNamesOutsideTheParent protects TheoryOfFuseMount: a
// name that carries a path separator or names ".." cannot name one directory
// entry, and joining it would walk out of the parent the request names. The
// kernel never sends such a name, so a request that does reports EINVAL and
// the path outside the parent stays untouched.
func TestFuseServerRejectsNamesOutsideTheParent(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "d/x", "x")
	writeHost(t, base, "outside", "o")
	c := newFuseClient(t, store)
	errno, resp := c.request(t, fuseOpLookup, fuseRootID, c.bodyWithNames(nil, "d"))
	if errno != 0 {
		t.Fatalf("lookup of /d: errno %d", errno)
	}
	var entry fuseEntryOut
	if err := binary.Read(bytes.NewReader(resp), binary.LittleEndian, &entry); err != nil {
		t.Fatal(err)
	}
	dNode := entry.Nodeid
	for _, name := range []string{"../outside", "x/../../outside", "/outside"} {
		if errno, _ := c.request(t, fuseOpLookup, dNode, c.bodyWithNames(nil, name)); errno != -int32(unix.EINVAL) {
			t.Errorf("lookup of %q: errno %d, want EINVAL", name, errno)
		}
		if errno, _ := c.request(t, fuseOpUnlink, dNode, c.bodyWithNames(nil, name)); errno != -int32(unix.EINVAL) {
			t.Errorf("unlink of %q: errno %d, want EINVAL", name, errno)
		}
	}
	// The names ".." and "." name no entry at all, so a LOOKUP answers ENOENT.
	for _, name := range []string{"..", "."} {
		if errno, _ := c.request(t, fuseOpLookup, dNode, c.bodyWithNames(nil, name)); errno != -int32(unix.ENOENT) {
			t.Errorf("lookup of %q: errno %d, want ENOENT", name, errno)
		}
	}
	// The file outside the parent is still where the base put it.
	if got, err := store.ReadFile("/outside"); err != nil || string(got) != "o" {
		t.Errorf("the file outside the parent changed: %q %v", got, err)
	}
	// A request that names one entry of the same parent still answers.
	if errno, _ := c.request(t, fuseOpLookup, dNode, c.bodyWithNames(nil, "x")); errno != 0 {
		t.Errorf("lookup of /d/x: errno %d, want success", errno)
	}
}
