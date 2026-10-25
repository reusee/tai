package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestTruncateFileReportsNotExistAndEINVAL protects TheoryOfCompletePOSIX:
// truncate(2) names an existing regular file, so a path in neither layer
// reports ENOENT rather than being created, and a negative length reports
// EINVAL rather than the size-bound refusal.
func TestTruncateFileReportsNotExistAndEINVAL(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.TruncateFile("/missing", 4, 0o644); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("truncate of a missing path: got %v, want a not-exist error", err)
	}
	if _, err := store.Stat("/missing"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused truncate left a row: %v", err)
	}
	if err := store.WriteFile("/f", []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.TruncateFile("/f", -1, 0o644); !errors.Is(err, syscall.EINVAL) {
		t.Errorf("truncate to a negative length: got %v, want EINVAL", err)
	}
	if a, err := store.Stat("/f"); err != nil || a.Size != 3 {
		t.Errorf("the refused truncate changed the file: size %d, error %v", a.Size, err)
	}
}

// TestWriteFileExplicitPermSetsTheMode pins WriteFile's perm contract with an
// explicit perm: the perm names the resulting mode whether the path is fresh or
// carries a whiteout row from an earlier session deletion. A directory rename
// relies on it, because every materialized entry takes its source's mode and a
// destination may already carry such a row. See TheoryOfCompletePOSIX.
func TestWriteFileExplicitPermSetsTheMode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remove bool
	}{
		{name: "fresh path"},
		{name: "over whiteout", remove: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, "base")
			if err := os.MkdirAll(base, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(base, "f"), []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := Open(filepath.Join(dir, "upper.db"), base)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if tc.remove {
				if err := store.Remove("/f"); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.WriteFile("/f", []byte("new"), 0o640); err != nil {
				t.Fatal(err)
			}
			a, err := store.Stat("/f")
			if err != nil {
				t.Fatal(err)
			}
			if a.Mode != 0o640 {
				t.Errorf("explicit perm: got mode %o, want 640", a.Mode)
			}
			if content, err := store.ReadFile("/f"); err != nil || string(content) != "new" {
				t.Errorf("content after the write: %q %v, want new", content, err)
			}
		})
	}
}
