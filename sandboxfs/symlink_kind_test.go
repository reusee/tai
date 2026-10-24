package sandboxfs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileOverSymlinkWithTheSameBytesIsAChange protects
// TheoryOfSandboxFilesystem: a base symbolic link records a tagged hash of its
// target, so a regular file whose bytes spell that target is still a change of
// kind. A bare digest would equal the file's own hash and the diff would
// report nothing.
func TestFileOverSymlinkWithTheSameBytesIsAChange(t *testing.T) {
	s, base := newTestStore(t)
	if err := os.Symlink("hello", filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("/link", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	c := findChange(t, changes, "/link")
	if c.Kind != ChangeModified {
		t.Fatalf("kind %q, want modified: a file over a link is a change of kind", c.Kind)
	}
	if c.Symlink {
		t.Error("the change reports a link, but the session wrote a regular file")
	}
	if string(c.Current) != "hello" {
		t.Errorf("current = %q, want hello", c.Current)
	}
	if c.Conflict != "" {
		t.Errorf("conflict = %q, want none", c.Conflict)
	}
}

// TestSymlinkOverFileWithTheSameBytesIsAChange pins the other direction: a
// session link whose target spells the base file's bytes is a change of kind.
func TestSymlinkOverFileWithTheSameBytesIsAChange(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "link", "hello")
	if err := s.Remove("/link"); err != nil {
		t.Fatal(err)
	}
	if err := s.Symlink("hello", "/link"); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	c := findChange(t, changes, "/link")
	if c.Kind != ChangeModified || !c.Symlink {
		t.Fatalf("change %+v, want a modified link", c)
	}
	if string(c.Current) != "hello" {
		t.Errorf("current = %q, want hello", c.Current)
	}
	if c.Conflict != "" {
		t.Errorf("conflict = %q, want none", c.Conflict)
	}
}
