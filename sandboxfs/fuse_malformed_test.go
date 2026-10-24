//go:build linux

package sandboxfs

import (
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
