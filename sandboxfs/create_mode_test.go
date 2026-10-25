//go:build linux

package sandboxfs

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// TestFuseServerCreateKeepsModeOfExistingFile protects TheoryOfCompletePOSIX:
// CREATE with O_TRUNC empties an existing file and changes nothing else. The
// mode the request carries describes a file the create would have made, so it
// must not replace the mode the file already has.
func TestFuseServerCreateKeepsModeOfExistingFile(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(base, "f")
	if err := os.WriteFile(host, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(host, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	srv := newFuseServer(store, -1)
	root, err := srv.nodeOf("/", true)
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, binary.Size(fuseCreateIn{}))
	binary.LittleEndian.PutUint32(body[0:], uint32(unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC))
	binary.LittleEndian.PutUint32(body[4:], 0o777)
	body = append(body, "f\x00"...)
	if _, err := srv.handleCreate(root, body); err != nil {
		t.Fatalf("create over an existing base file: %v", err)
	}
	a, err := store.Stat("/f")
	if err != nil {
		t.Fatal(err)
	}
	if a.Mode != 0o600 {
		t.Errorf("the truncating create changed the mode: got %o, want 600", a.Mode)
	}
	if a.Size != 0 {
		t.Errorf("the truncating create left size %d, want 0", a.Size)
	}
}

// TestFuseServerCreateWithModeZeroKeepsZeroBits protects TheoryOfCompletePOSIX:
// the mode a create, mkdir, or mknod request carries is the mode the caller
// chose, and a mode of no bits is a choice. A handler that substitutes 0644 or
// 0755 hands the store bits the caller never asked for, and the write path
// keeps the handle's recorded mode across a delete-and-write.
func TestFuseServerCreateWithModeZeroKeepsZeroBits(t *testing.T) {
	store, _ := newTestStore(t)
	c := newFuseClient(t, store)

	createIn := fuseCreateIn{Flags: uint32(unix.O_WRONLY | unix.O_CREAT), Mode: 0}
	if errno, _ := c.request(t, fuseOpCreate, fuseRootID,
		c.bodyWithNames(encodeStruct(&createIn), "f")); errno != 0 {
		t.Fatalf("create with mode 0: errno %d", errno)
	}
	if a, err := store.Stat("/f"); err != nil || a.Mode != 0 {
		t.Errorf("create with mode 0: got mode %o (error %v), want 000", a.Mode, err)
	}

	mkdirIn := fuseMkdirIn{Mode: 0}
	if errno, _ := c.request(t, fuseOpMkdir, fuseRootID,
		c.bodyWithNames(encodeStruct(&mkdirIn), "d")); errno != 0 {
		t.Fatalf("mkdir with mode 0: errno %d", errno)
	}
	if a, err := store.Stat("/d"); err != nil || a.Mode != 0 {
		t.Errorf("mkdir with mode 0: got mode %o (error %v), want 000", a.Mode, err)
	}

	mknodIn := fuseMknodIn{Mode: uint32(syscall.S_IFIFO)}
	if errno, _ := c.request(t, fuseOpMknod, fuseRootID,
		c.bodyWithNames(encodeStruct(&mknodIn), "p")); errno != 0 {
		t.Fatalf("mknod with mode 0: errno %d", errno)
	}
	if a, err := store.Stat("/p"); err != nil || a.Mode != 0 {
		t.Errorf("mknod with mode 0: got mode %o (error %v), want 000", a.Mode, err)
	}

	// A write through a handle on a mode-0 file keeps the mode across the
	// delete-and-write the store's path model reaches.
	if err := store.Remove("/f"); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/f", 0, []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if a, err := store.Stat("/f"); err != nil || a.Mode != 0 || a.Size != 1 {
		t.Errorf("splice after the delete: mode %o size %d (error %v), want mode 000 size 1",
			a.Mode, a.Size, err)
	}
}
