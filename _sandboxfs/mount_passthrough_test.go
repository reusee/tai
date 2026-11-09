//go:build linux

package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestBindPassthroughSkipsAMissingSource protects TheoryOfPassthrough: a
// passthrough whose source is missing is skipped, and the skip leaves no trace.
// A target directory created for the mount that never happens would enter the
// store as a change the session never made, so the source is resolved first.
func TestBindPassthroughSkipsAMissingSource(t *testing.T) {
	base := t.TempDir()
	store, err := OpenDir(filepath.Join(t.TempDir(), "rec.d"), base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m := &MountedFS{Store: store, Mountpoint: t.TempDir()}
	missing := filepath.Join(base, "missing-passthrough")
	if err := m.bindPassthrough([]string{missing}); err != nil {
		t.Fatalf("a missing source must be skipped without an error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.Mountpoint, missing)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing passthrough left a target behind: %v", err)
	}
}

// TestBindPassthroughRemountsABindThatInheritedReadOnly pins the
// passthrough's answer to the hardened namespace: the container marks
// read-only every mount outside its writable set, a bind copies the
// source's read-only flag, and the cache writer the passthrough serves
// must still write through it. The source here is a per-mount read-only
// bind over a writable filesystem, so the remount clears the flag; a
// source read-only at the superblock keeps its bind read-only.
// See TheoryOfPassthrough.
func TestBindPassthroughRemountsABindThatInheritedReadOnly(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "cache")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// The source stands on its own read-only bind, the same stand the
	// hardened namespace leaves outside its writable set.
	if err := unix.Mount(src, src, "", unix.MS_BIND, ""); err != nil {
		t.Skipf("mounting is unavailable here: %v", err)
	}
	t.Cleanup(func() { _ = unix.Unmount(src, unix.MNT_DETACH) })
	if err := unix.Mount(src, src, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
		t.Fatalf("remount the source read-only: %v", err)
	}
	m := &MountedFS{Mountpoint: t.TempDir()}
	target := filepath.Join(m.Mountpoint, src)
	t.Cleanup(func() { _ = unix.Unmount(target, unix.MNT_DETACH) })
	if err := m.bindPassthrough([]string{src}); err != nil {
		t.Fatalf("bind passthrough %s: %v", src, err)
	}
	probe := filepath.Join(target, "probe")
	if err := os.WriteFile(probe, []byte("cache writer"), 0o644); err != nil {
		t.Fatalf("write through the passthrough bind: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(src, "probe"))
	if err != nil || string(got) != "cache writer" {
		t.Fatalf("the write did not land in the source: %v, %q", err, got)
	}
}

// TestPassthroughAlwaysCarriesTheKernelFilesystems protects
// TheoryOfPassthrough: /dev, /proc, and /sys must stay real kernel
// filesystems inside the mount whatever the caller passes. A store that
// served /dev would report a character device as a regular file and open it
// to hash its content — a terminal blocks until a key arrives and the read
// consumes what it waited for — and the server answers every request on one
// goroutine, so the whole filesystem would freeze behind it.
func TestPassthroughAlwaysCarriesTheKernelFilesystems(t *testing.T) {
	got := passthroughPaths(nil)
	for _, want := range kernelFilesystems {
		found := false
		for _, p := range got {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("passthrough paths %v hold no %q", got, want)
		}
	}
	// A caller's own directory still passes through beside them.
	got = passthroughPaths([]string{"/var/cache"})
	if len(got) != len(kernelFilesystems)+1 || got[len(got)-1] != "/var/cache" {
		t.Fatalf("passthrough paths %v dropped the caller's directory", got)
	}
}
