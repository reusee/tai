//go:build linux

package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestBindPassthroughSkipsAMissingSource protects TheoryOfPassthrough: a
// passthrough whose source is missing is skipped, and the skip leaves no trace.
// A target directory created for the mount that never happens would enter the
// store as a change the session never made, so the source is resolved first.
func TestBindPassthroughSkipsAMissingSource(t *testing.T) {
	base := t.TempDir()
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), base)
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
