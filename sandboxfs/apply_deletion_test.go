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
