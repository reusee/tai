package sandboxfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWritePolicyCoversEveryMutation protects TheoryOfWritePolicy: the store
// asks the policy about every path it is about to mutate, before it changes
// anything, so a refused path is left untouched.
func TestWritePolicyCoversEveryMutation(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(filepath.Join(base, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	const refused = "/work/f"
	store, err := OpenWithPolicy(filepath.Join(dir, "upper.db"), base, func(p string) error {
		if p == refused {
			return fmt.Errorf("%w: %s", os.ErrPermission, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.WriteFile("/work/ok", []byte("ok"), 0o644); err != nil {
		t.Fatalf("the policy admits /work/ok, so the write must succeed: %v", err)
	}
	now := time.Now()
	ops := []struct {
		name string
		call func() error
	}{
		{"write", func() error { return store.WriteFile(refused, []byte("x"), 0o644) }},
		{"splice", func() error { return store.SpliceFile(refused, 0, []byte("x"), 0o644) }},
		{"truncate", func() error { return store.TruncateFile(refused, 0, 0o644) }},
		{"mkdir", func() error { return store.Mkdir(refused, 0o755) }},
		{"symlink", func() error { return store.Symlink("target", refused) }},
		{"mkfifo", func() error { return store.Mkfifo(refused, 0o644) }},
		{"chmod", func() error { return store.SetMode(refused, 0o600) }},
		{"utimes", func() error { return store.SetTimes(refused, now) }},
		{"remove", func() error { return store.Remove(refused) }},
	}
	for _, op := range ops {
		if err := op.call(); !errors.Is(err, os.ErrPermission) {
			t.Errorf("%s: got %v, want a permission error", op.name, err)
		}
	}
	if _, err := store.Stat(refused); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused path left a trace: %v", err)
	}
}

// TestSpliceFileZeroLengthKeepsTheLength protects TheoryOfCompletePOSIX: a
// zero-byte write transfers nothing and does not extend the file.
func TestSpliceFileZeroLengthKeepsTheLength(t *testing.T) {
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
	if err := store.WriteFile("/f", []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/f", 100, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := store.Stat("/f")
	if err != nil {
		t.Fatal(err)
	}
	if a.Size != 3 {
		t.Errorf("size after a zero-byte write at offset 100: got %d, want 3", a.Size)
	}
	content, err := store.ReadFile("/f")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "abc" {
		t.Errorf("content after a zero-byte write: got %q, want %q", content, "abc")
	}
}

// TestOwnDatabaseIsRefusedThroughItsBasePath protects
// TheoryOfSandboxFilesystem: the store's own database is never a sandbox
// target, and a base that holds it gives the file a second path.
func TestOwnDatabaseIsRefusedThroughItsBasePath(t *testing.T) {
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
	name := "/" + filepath.Base(dbPath)
	if err := store.WriteFile(name, []byte("not a database"), 0o644); !errors.Is(err, os.ErrPermission) {
		t.Errorf("writing the store database through %s: got %v, want a permission error", name, err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Path == name {
			t.Errorf("the store database appears as a change: %s", c.Path)
		}
	}
}

// TestSetModeAndSetTimesOnMissingPathReportNotExist protects
// TheoryOfCompletePOSIX: a mode or a time change names a path that must exist,
// so a path that exists in neither layer reports ENOENT and leaves no row.
func TestSetModeAndSetTimesOnMissingPathReportNotExist(t *testing.T) {
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
	if err := store.SetMode("/missing", 0o600); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("chmod of a missing path: got %v, want a not-exist error", err)
	}
	if err := store.SetTimes("/missing", time.Now()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("utimes of a missing path: got %v, want a not-exist error", err)
	}
	if _, err := store.Stat("/missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused change left a row: %v", err)
	}
}

// TestDirectoryOverBaseFileIsAChange protects TheoryOfSandboxFilesystem: a
// directory the session puts where the base held a file is a net change, and
// applying it replaces the host file. A base directory records no content
// hash, so the recorded hash is what tells the two apart.
func TestDirectoryOverBaseFileIsAChange(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "d"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Remove("/d"); err != nil {
		t.Fatal(err)
	}
	if err := store.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "/d" || !changes[0].Dir {
		t.Fatalf("a directory over a base file: got %+v, want one directory change for /d", changes)
	}
	if _, err := store.Apply(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(base, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Errorf("applying the change left %v, want a directory", info.Mode())
	}
}

// TestApplyRejectsHostKindChange protects TheoryOfApplyConflict: a host path
// that turned into a directory after the session snapshot blocks the change,
// and nothing is applied.
func TestApplyRejectsHostKindChange(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.WriteFile("/f", []byte("session"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(base, "f")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "f"), 0o755); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Conflict == "" {
		t.Fatalf("a host kind change: got %+v, want one conflicting change", changes)
	}
	applied, err := store.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Errorf("a conflicting change was applied: %+v", applied)
	}
}
