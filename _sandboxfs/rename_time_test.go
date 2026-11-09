package sandboxfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRenameKeepsTheFileTime protects TheoryOfCompletePOSIX: POSIX gives
// rename(2) no mtime effect, and a toolchain that reads mtime to judge
// staleness must not see a renamed file as freshly written. Both sources
// are covered: a base file keeps the time its host inode recorded, and an
// upper file keeps the time the session set on it.
func TestRenameKeepsTheFileTime(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "x")
	want := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(filepath.Join(base, "a.txt"), want, want); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("/a.txt", "/b.txt"); err != nil {
		t.Fatal(err)
	}
	a, err := s.Stat("/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !a.ModTime.Equal(want) {
		t.Errorf("renaming a base file changed its mtime: got %v, want %v", a.ModTime, want)
	}
	set := time.Unix(1_500_000_000, 0)
	if err := s.SetTimes("/b.txt", set); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("/b.txt", "/c.txt"); err != nil {
		t.Fatal(err)
	}
	if a, err = s.Stat("/c.txt"); err != nil {
		t.Fatal(err)
	}
	if !a.ModTime.Equal(set) {
		t.Errorf("renaming an upper file changed its mtime: got %v, want %v", a.ModTime, set)
	}
}

// TestRenameDirectoryKeepsTheTimes protects the same rule inside a
// directory move: renameDir recreates every entry under the new name, and
// each recreation must carry the entry's own time instead of the session's
// clock.
func TestRenameDirectoryKeepsTheTimes(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "src/a.txt", "a")
	want := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(filepath.Join(base, "src", "a.txt"), want, want); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("/src", "/dst"); err != nil {
		t.Fatal(err)
	}
	a, err := s.Stat("/dst/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !a.ModTime.Equal(want) {
		t.Errorf("directory rename changed the file's mtime: got %v, want %v", a.ModTime, want)
	}
}

// TestLinkSharesTheFileTime protects the same rule for a hard link: the
// store holds no shared inode, but the linked name still reports the
// source node's time, because POSIX gives link(2) no mtime effect.
func TestLinkSharesTheFileTime(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "hello")
	want := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(filepath.Join(base, "a.txt"), want, want); err != nil {
		t.Fatal(err)
	}
	if err := s.Link("/a.txt", "/b.txt"); err != nil {
		t.Fatal(err)
	}
	a, err := s.Stat("/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !a.ModTime.Equal(want) {
		t.Errorf("the linked name lost the source's mtime: got %v, want %v", a.ModTime, want)
	}
}
