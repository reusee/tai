//go:build linux

package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestMountRefusesItsOwnDatabase protects TheoryOfSandboxFilesystem: a
// database that lives inside the base reaches the sandbox through the mount,
// and the store still refuses to mutate it. The mount's base is a private bind
// view, so a text comparison between the sandbox path and the host path the
// database was opened with cannot find the match; the mount records the
// database's view name so the identity check does. The test skips when the
// environment cannot mount FUSE.
func TestMountRefusesItsOwnDatabase(t *testing.T) {
	hostDir := t.TempDir()
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	dbPath := filepath.Join(hostDir, ".db")
	mfs, err := Mount(MountOptions{
		Base:       hostDir,
		DBPath:     dbPath,
		Mountpoint: mountPoint,
	})
	if err != nil {
		switch {
		case errors.Is(err, syscall.EPERM),
			errors.Is(err, syscall.EACCES),
			errors.Is(err, syscall.ENODEV),
			errors.Is(err, syscall.ENOENT),
			errors.Is(err, syscall.ENOSYS):
			t.Skipf("fuse mount unavailable: %v", err)
		}
		t.Fatal(err)
	}
	defer func() {
		if err := mfs.Unmount(); err != nil {
			t.Errorf("unmount: %v", err)
		}
	}()
	name := "/" + filepath.Base(dbPath)
	if err := mfs.Store.WriteFile(name, []byte("x"), 0o644); !errors.Is(err, os.ErrPermission) {
		t.Errorf("write over the mounted database through %s: got %v, want a permission error", name, err)
	}
	if err := mfs.Store.Remove(name); !errors.Is(err, os.ErrPermission) {
		t.Errorf("remove of the mounted database through %s: got %v, want a permission error", name, err)
	}
}
