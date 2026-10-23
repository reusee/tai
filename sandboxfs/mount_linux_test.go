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
