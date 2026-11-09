package sandboxfs

import (
	"testing"
)

// TestMkdirOverContentClearsSize protects TheoryOfChunkedContent: only a
// regular row carries content, so a directory the session puts where a file
// stood reports size zero, and a listing and a stat agree on the size.
func TestMkdirOverContentClearsSize(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a", "x")
	if err := s.WriteFile("/a", make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("/a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Mkdir("/a", 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := s.Stat("/a")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindDir {
		t.Fatalf("kind %v, want a directory", a.Kind)
	}
	if a.Size != 0 {
		t.Errorf("stat size %d, want 0 for a directory", a.Size)
	}
	entries, err := s.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "a" {
			if e.Attr.Size != a.Size {
				t.Errorf("listing size %d, stat size %d: the two must agree", e.Attr.Size, a.Size)
			}
			return
		}
	}
	t.Fatal("the listing holds no entry for /a")
}

// TestMkfifoOverContentClearsSize protects TheoryOfCompletePOSIX: a pipe
// carries no content, so a pipe the session puts where a file stood reports
// size zero on both a stat and a listing.
func TestMkfifoOverContentClearsSize(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a", "x")
	if err := s.WriteFile("/a", make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("/a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Mkfifo("/a", 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := s.Stat("/a")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindFIFO {
		t.Fatalf("kind %v, want a pipe", a.Kind)
	}
	if a.Size != 0 {
		t.Errorf("stat size %d, want 0 for a pipe", a.Size)
	}
	entries, err := s.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "a" {
			if e.Attr.Size != a.Size {
				t.Errorf("listing size %d, stat size %d: the two must agree", e.Attr.Size, a.Size)
			}
			return
		}
	}
	t.Fatal("the listing holds no entry for /a")
}
