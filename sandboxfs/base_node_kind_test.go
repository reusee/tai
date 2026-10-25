//go:build linux

package sandboxfs

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestBaseCaptureRefusesANodeItCannotServe protects TheoryOfBaseCapture: the
// store records the base state of a regular file, a directory, a link, or a
// pipe, and refuses every other node before it opens one. A character device
// blocks on read until its peer writes — the controlling terminal blocks until
// a key arrives — and that read consumes the bytes it waited for, so a store
// that hashed a device would swallow the user's input and freeze every other
// process behind the server's one goroutine.
func TestBaseCaptureRefusesANodeItCannotServe(t *testing.T) {
	base := t.TempDir()
	sock := filepath.Join(base, "sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.WriteFile("/sock", []byte("x"), 0o644); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("write over a socket: got %v, want EINVAL", err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("the refused write recorded %d change(s)", len(changes))
	}

	// The character device is the node the field hit. /dev/null answers a read
	// without blocking, so the test proves the refusal without waiting on a
	// terminal.
	fi, err := os.Lstat("/dev/null")
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		t.Skip("no character device to test with")
	}
	dev, err := Open(filepath.Join(t.TempDir(), "dev.db"), "/dev")
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	if err := dev.WriteFile("/null", []byte("x"), 0o644); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("write over a character device: got %v, want EINVAL", err)
	}
}

// TestReadHandleRefusesADeviceThroughThePortableWalk protects
// TheoryOfBaseCapture on the one open path the base descriptor does not cover:
// the portable walk. The store's idea of the kind can come from an upper row,
// so the base node is read before the open. Opening a character device waits
// for its peer's data — a terminal waits for a key — and the read that follows
// consumes it, so a server that opened one would swallow the user's input and
// hold its only goroutine.
func TestReadHandleRefusesADeviceThroughThePortableWalk(t *testing.T) {
	fi, err := os.Lstat("/dev/null")
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		t.Skip("no character device to test with")
	}
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), "/dev")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// The portable walk is the path a platform without the base descriptor
	// takes; forcing it here exercises the same code on Linux.
	closeBaseDirFd(store.baseFd)
	store.baseFd = -1
	s := newFuseServer(store, -1)
	h := &fuseHandle{path: "/null"}
	if _, err := s.baseHandleFile(h); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("a read handle on a character device: got %v, want EINVAL", err)
	}
	if h.base != nil {
		t.Fatal("the refused open left a cached reader on the handle")
	}
}
