package sandboxfs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestChangesFillsOriginalOnDeletion protects TheoryOfApplyConflict: a
// deletion whose host path still matches the snapshot reports the deleted
// bytes as Original, the same content a modification reports. A deleted link's
// Original is its target.
func TestChangesFillsOriginalOnDeletion(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		s, base := newTestStore(t)
		writeHost(t, base, "a.txt", "original")
		if err := s.Remove("/a.txt"); err != nil {
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
		if string(c.Original) != "original" {
			t.Errorf("deletion Original = %q, want original", c.Original)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		s, base := newTestStore(t)
		if err := os.Symlink("target.txt", filepath.Join(base, "link")); err != nil {
			t.Fatal(err)
		}
		if err := s.Remove("/link"); err != nil {
			t.Fatal(err)
		}
		changes, err := s.Changes()
		if err != nil {
			t.Fatal(err)
		}
		c := findChange(t, changes, "/link")
		if c.Kind != ChangeDeleted || !c.Symlink {
			t.Fatalf("change %+v, want a deleted symlink", c)
		}
		if string(c.Original) != "target.txt" {
			t.Errorf("symlink deletion Original = %q, want target.txt", c.Original)
		}
	})
}
