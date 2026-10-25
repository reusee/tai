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
