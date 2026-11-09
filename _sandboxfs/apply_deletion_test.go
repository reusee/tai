package sandboxfs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestApplyDeletionOfAGoneHostPathIsANoOp protects TheoryOfApplyConflict: a
// session deletion whose host path is already gone is an agreement, not a
// conflict, so the change is applied as a no-op and dropped from the upper
// layer.
func TestApplyDeletionOfAGoneHostPathIsANoOp(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "x")
	if err := s.Remove("/a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(base, "a.txt")); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	c := findChange(t, changes, "/a.txt")
	if c.Kind != ChangeDeleted {
		t.Fatalf("kind %q, want deleted", c.Kind)
	}
	if c.Conflict != "" {
		t.Errorf("deleted-and-gone path reported conflict %q, want none", c.Conflict)
	}
	applied, err := s.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0].Path != "/a.txt" {
		t.Errorf("applied %+v, want the one no-op deletion", applied)
	}
}

// TestApplyFileOverDirectoryWithDeletedEntries protects TheoryOfApplyConflict:
// a session that empties a directory, removes it, and writes a regular file at
// the same path applies cleanly. The deletion of the directory's entries must
// run before the file's own change, or the host directory still holds them and
// the write lands on a non-empty directory. See applyPhase.
func TestApplyFileOverDirectoryWithDeletedEntries(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "d/f.txt", "x")
	if err := s.Remove("/d/f.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("/d"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("/d", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	applied, err := s.Apply(nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied %+v, want the deletion and the file", applied)
	}
	info, err := os.Lstat(filepath.Join(base, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("after apply /d is %v, want a regular file", info.Mode())
	}
	got, err := os.ReadFile(filepath.Join(base, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Fatalf("content %q, want hi", got)
	}
}
