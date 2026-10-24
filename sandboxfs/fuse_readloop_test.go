//go:build linux

package sandboxfs

import (
	"io"
	"testing"

	"golang.org/x/sys/unix"
)

// TestFuseServerSurvivesInterruptedRead protects TheoryOfFuseMount: the FUSE
// device reports ENOENT when a caller interrupts its request before the server
// reads it. The answer is to read the next request; returning would kill the
// daemon, and every later filesystem call through the mount would fail with
// EIO.
func TestFuseServerSurvivesInterruptedRead(t *testing.T) {
	store, _ := newTestStore(t)
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = unix.Close(fds[0])
		_ = unix.Close(fds[1])
	})
	s := newFuseServer(store, fds[0])
	msg := requestBytes(9, fuseOpStatfs, fuseRootID, nil)
	var calls int
	s.readReq = func(buf []byte) (int, error) {
		calls++
		switch calls {
		case 1:
			// The interruption the device reports for a request nobody read.
			return 0, unix.ENOENT
		case 2:
			return copy(buf, msg), nil
		default:
			return 0, io.EOF
		}
	}
	done := make(chan struct{})
	go func() {
		s.run()
		close(done)
	}()
	errno, unique := readReply(t, fds[1])
	<-done
	if unique != 9 {
		t.Errorf("reply tag %d, want 9", unique)
	}
	if errno != 0 {
		t.Errorf("statfs after the interrupted read: errno %d, want 0", errno)
	}
}
