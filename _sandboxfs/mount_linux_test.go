//go:build linux

package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestMountServesRootThroughKernel mounts the filesystem over a temporary
// host directory and enters the mountpoint, the first step of the root
// pivot. The socketpair tests speak the protocol to the server directly;
// only a real mount crosses the kernel, so only this test exercises the
// INIT handshake — above all the fuse_init_out field layout, which the
// kernel parses by its own struct offsets. A field order that disagrees
// with the kernel leaves max_write and max_background zero and the
// connection unusable, so entering the mountpoint fails. The test skips
// when the environment cannot mount FUSE. See TheoryOfFuseMount.
func TestMountServesRootThroughKernel(t *testing.T) {
	hostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostDir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	mfs, err := Mount(MountOptions{
		Base:       hostDir,
		DBPath:     filepath.Join(t.TempDir(), "sandbox.db"),
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

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(mountPoint); err != nil {
		t.Fatalf("chdir into the mountpoint: %v", err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	info, err := os.Stat(".")
	if err != nil {
		t.Fatalf("stat the mount root: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("mount root is not a directory: %v", info.Mode())
	}
	content, err := os.ReadFile("hello.txt")
	if err != nil {
		t.Fatalf("read a base file through the mount: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("read a base file through the mount: %q", content)
	}
}

// TestActivateRecordsTheActiveStoreAfterAPivot protects TheoryOfActiveStore:
// the store that serves the process root is recorded by the call that mounts
// and pivots, and only after the pivot succeeded, so a process whose pivot
// failed carries no store. The pivot is a parameter of the step, because no
// test can move the process root and then reach its own files again.
func TestActivateRecordsTheActiveStoreAfterAPivot(t *testing.T) {
	store, _ := newTestStore(t)
	defer SetActiveStore(nil)
	m := &MountedFS{Store: store, Mountpoint: "/mnt"}

	// A mount without a pivot is not the process root, so it records nothing.
	if err := m.activate(false, func(string) error {
		t.Fatal("the step pivoted a mount that asked for no pivot")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := ActiveStore(); got != nil {
		t.Fatalf("a mount without a pivot recorded %p", got)
	}

	// A pivot that fails leaves no store: every consumer degrades to "no
	// sandbox" without a flag.
	failed := errors.New("pivot failed")
	if err := m.activate(true, func(string) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("a failed pivot returned %v, want %v", err, failed)
	}
	if got := ActiveStore(); got != nil {
		t.Fatalf("a failed pivot recorded %p", got)
	}

	// A pivot that succeeds records the store that serves the new root.
	pivoted := ""
	if err := m.activate(true, func(p string) error { pivoted = p; return nil }); err != nil {
		t.Fatal(err)
	}
	if pivoted != m.Mountpoint {
		t.Errorf("the step pivoted %q, want the mountpoint %q", pivoted, m.Mountpoint)
	}
	if got := ActiveStore(); got != store {
		t.Fatalf("the active store is %p, want the mounted store %p", got, store)
	}
}
