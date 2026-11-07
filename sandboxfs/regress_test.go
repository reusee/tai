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
	store, err := Open(filepath.Join(dir, "upper.db"), base)
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
	store, err := Open(filepath.Join(dir, "upper.db"), base)
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

// TestCheckWriteRefusesOwnDatabaseWithAStaleRow protects
// TheoryOfSandboxFilesystem: the store never mutates its own database, even
// when a stale row from an older schema leaves one. The refusal belongs to
// the common mutation entry, so no path that skips captureOriginal can slip
// through. The row is injected through the sqlite layer's connection; a
// memory layer carries no record to protect, so the test names the sqlite one.
func TestCheckWriteRefusesOwnDatabaseWithAStaleRow(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := DefaultDBPath(base)
	store, err := Open(dbPath, base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u := sqliteOf(t, store)
	rel, err := filepath.Rel(base, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	name := "/" + filepath.ToSlash(rel)
	// A row for the database path makes the mutation path skip
	// captureOriginal, which was the older code's only refusal.
	if _, err := u.db.Exec(
		`INSERT INTO files (path, kind, mode, mtime, deleted, size, target, original_exists, original_hash)
		 VALUES (?, 0, 420, 0, 0, 0, '', 0, '')`, name); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile(name, []byte("corrupt"), 0o644); !errors.Is(err, os.ErrPermission) {
		t.Errorf("write over a stale database row: got %v, want a permission error", err)
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

// TestOwnDatabaseOutsideBaseIsRefusedThroughHardLink protects
// TheoryOfSandboxFilesystem: record protection is by file identity, not only
// by path text. A database outside the base is still the store's own record
// when a hard link inside the base names the same inode.
func TestOwnDatabaseOutsideBaseIsRefusedThroughHardLink(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "record.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := os.Link(filepath.Join(dir, "record.db"), filepath.Join(base, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile("/alias", []byte("corrupt"), 0o600); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("write through a hard link to the record: got %v, want a permission error", err)
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
