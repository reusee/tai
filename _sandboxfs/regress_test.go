package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSetTimesKeepsBaseFileMode protects TheoryOfCompletePOSIX: a time-only
// change records the time and leaves every other attribute alone. The
// materialisation of the base content must not replace the file's mode with
// the store's default.
func TestSetTimesKeepsBaseFileMode(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(base, "f")
	if err := os.WriteFile(host, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(host, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenDir(filepath.Join(dir, "upper.d"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetTimes("/f", time.Now()); err != nil {
		t.Fatal(err)
	}
	a, err := store.Stat("/f")
	if err != nil {
		t.Fatal(err)
	}
	if a.Mode != 0o600 {
		t.Errorf("utimes changed the mode: got %o, want 600", a.Mode)
	}
	if content, err := store.ReadFile("/f"); err != nil || string(content) != "x" {
		t.Errorf("utimes changed the content: %q %v", content, err)
	}
}

// TestSpliceFileAfterRemovedDirectoryCreatesFile protects
// TheoryOfCompletePOSIX: once the session removed a directory, the name is
// free, and a write to it creates a regular file. The whiteout keeps the kind
// of the row it replaced, and that kind must not refuse the new file.
func TestSpliceFileAfterRemovedDirectoryCreatesFile(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := OpenDir(filepath.Join(dir, "upper.d"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("/d"); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/d", 0, []byte("x"), 0o644); err != nil {
		t.Fatalf("a write after the directory's deletion must create a file: %v", err)
	}
	a, err := store.Stat("/d")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindRegular || a.Size != 1 {
		t.Errorf("after the write: got kind %v size %d, want a regular file of one byte", a.Kind, a.Size)
	}
}

// TestCheckWriteRefusesOwnRecordWithAStaleRow protects
// TheoryOfSandboxFilesystem: the store never mutates its own record, even
// when a stale row from an older session leaves one. The refusal belongs to
// the common mutation entry, so no path that skips captureOriginal can slip
// through. The row is written straight into the record directory; a memory
// layer carries no record to protect, so the test names the directory layer.
func TestCheckWriteRefusesOwnRecordWithAStaleRow(t *testing.T) {
	base := t.TempDir()
	record := filepath.Join(base, "rec.d")
	store, err := OpenDir(record, base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, ok := store.upper.(*dirUpper)
	if !ok {
		t.Fatalf("store upper layer is %T, want *dirUpper", store.upper)
	}
	// A row beneath the record makes the mutation path skip captureOriginal,
	// which was the older code's only refusal.
	if err := u.writeMeta("/rec.d/x", dirRowOf(fileRow{
		path: "/rec.d/x", kind: KindRegular, mode: 0o644,
	})); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile("/rec.d/x", []byte("corrupt"), 0o644); !errors.Is(err, os.ErrPermission) {
		t.Errorf("write over a stale record row: got %v, want a permission error", err)
	}
}

// TestMetadataOnlyMutationAfterBaseFileVanishes protects TheoryOfBaseCapture:
// a metadata-only row still names the base file whose bytes it captured. Once
// that file vanishes, a content mutation must report the missing base instead
// of silently replacing the captured content with an empty file.
func TestMetadataOnlyMutationAfterBaseFileVanishes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Store) error
	}{
		{"splice", func(s *Store) error {
			return s.SpliceFile("/f", 0, []byte("z"), 0o600)
		}},
		{"truncate", func(s *Store) error {
			return s.TruncateFile("/f", 1, 0o600)
		}},
		{"rename", func(s *Store) error {
			return s.Rename("/f", "/g")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, base := newTestStore(t)
			writeHost(t, base, "f", "captured")
			if err := store.SetMode("/f", 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(base, "f")); err != nil {
				t.Fatal(err)
			}
			if err := tc.mutate(store); !errors.Is(err, ErrNotExist) {
				t.Fatalf("mutation after the base file vanished: got %v, want a not-exist error", err)
			}
		})
	}
}

// TestOwnRecordAliasIsRefused protects TheoryOfSandboxFilesystem: record
// protection is by file identity as well as by path text. A record outside
// the base is still the store's own record when a name inside the base
// aliases the record directory, and a write through that name is refused.
func TestOwnRecordAliasIsRefused(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "record.d")
	store, err := OpenDir(record, base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := os.Symlink(record, filepath.Join(base, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile("/alias", []byte("corrupt"), 0o600); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("write through an alias of the record: got %v, want a permission error", err)
	}
}

// TestDirRecordAtBaseProtectsTheWholeTree protects TheoryOfDirRecord: when
// the record directory is the base itself, every sandbox path is beneath the
// record and therefore outside the session's writable reach.
func TestDirRecordAtBaseProtectsTheWholeTree(t *testing.T) {
	base := t.TempDir()
	store, err := OpenDir(base, base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Mkdir("/work", 0o755); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("write beneath a record that is the whole base: got %v, want a permission error", err)
	}
}
