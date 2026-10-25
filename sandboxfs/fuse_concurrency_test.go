//go:build linux

package sandboxfs

import (
	"encoding/binary"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// fuseRequestBytes builds one request the server can dispatch: the wire
// header, then the opcode's fixed body.
func fuseRequestBytes(opcode uint32, unique, nodeid uint64, body []byte) []byte {
	p := make([]byte, fuseInHeaderSize+len(body))
	binary.LittleEndian.PutUint32(p[0:4], uint32(len(p)))
	binary.LittleEndian.PutUint32(p[4:8], opcode)
	binary.LittleEndian.PutUint64(p[8:16], unique)
	binary.LittleEndian.PutUint64(p[16:24], nodeid)
	copy(p[fuseInHeaderSize:], body)
	return p
}

// fuseReadBody builds one fuse_read_in body: a handle number and a window.
func fuseReadBody(fh uint64, size uint32) []byte {
	b := make([]byte, binary.Size(fuseReadIn{}))
	binary.LittleEndian.PutUint64(b[0:8], fh)
	binary.LittleEndian.PutUint32(b[16:20], size)
	return b
}

// TestFuseServerAnswersWhileHandleRequestBlocks proves the request pool: a
// handle-scoped request held open must not stop a path-scoped request from
// being answered. A single-goroutine server fails this test, and that stall is
// the whole process's stall — the kernel queues the requests of every process
// in the sandbox behind the one being answered. See TheoryOfFuseConcurrency.
func TestFuseServerAnswersWhileHandleRequestBlocks(t *testing.T) {
	devFd, err := unix.Open("/dev/null", unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(devFd)
	s := newFuseServer(nil, devFd)

	blocked := make(chan struct{})
	answered := make(chan struct{})
	release := make(chan struct{})
	s.beforeDispatch = func(opcode uint32) {
		switch opcode {
		case fuseOpRead:
			close(blocked)
			<-release
		case fuseOpStatfs:
			close(answered)
		}
	}

	queue := [][]byte{
		fuseRequestBytes(fuseOpRead, 1, fuseRootID, fuseReadBody(1, 16)),
		fuseRequestBytes(fuseOpStatfs, 2, fuseRootID, nil),
	}
	s.readReq = func(buf []byte) (int, error) {
		if len(queue) == 0 {
			return 0, unix.EBADF
		}
		n := copy(buf, queue[0])
		queue = queue[1:]
		return n, nil
	}
	go s.run()

	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("no request reached the pool")
	}
	select {
	case <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("a path request waited behind a blocked handle request")
	}
	close(release)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not stop")
	}
}
