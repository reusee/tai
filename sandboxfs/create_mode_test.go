//go:build linux

package sandboxfs

import (
	"encoding/binary"
	"os"
	"path/filepath"
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
