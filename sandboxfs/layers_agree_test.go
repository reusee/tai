package sandboxfs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// newLayerStores opens one store per upper layer over the same base directory,
// so a test can run one operation sequence against all three and compare what a
// caller sees. See TheoryOfUpperLayer.
func newLayerStores(t *testing.T) map[string]*Store {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	mem, err := OpenMemory(base)
	if err != nil {
		t.Fatal(err)
	}
	sql, err := Open(filepath.Join(dir, "record.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := OpenDir(filepath.Join(dir, "record"), base)
	if err != nil {
		t.Fatal(err)
	}
	stores := map[string]*Store{"memory": mem, "sql": sql, "dir": rec}
	t.Cleanup(func() {
		for _, s := range stores {
			_ = s.Close()
		}
	})
	return stores
}

// layerView renders everything a caller can observe about one store: the
// attribute of each named path, the content of each regular file, the target of
// each link, and both directory listings. The time is left out because each
// layer stamps its own clock.
func layerView(t *testing.T, s *Store, paths []string) string {
	t.Helper()
	var b bytes.Buffer
	for _, p := range paths {
		a, err := s.Stat(p)
		if err != nil {
			fmt.Fprintf(&b, "stat %s: %v\n", p, err)
			continue
		}
		fmt.Fprintf(&b, "stat %s: kind %d mode %o size %d\n", p, a.Kind, a.Mode, a.Size)
		switch a.Kind {
		case KindRegular:
			content, err := s.ReadFile(p)
			if err != nil {
				fmt.Fprintf(&b, "read %s: %v\n", p, err)
				continue
			}
			fmt.Fprintf(&b, "read %s: %d bytes %s\n", p, len(content), contentHash(content)[:16])
		case KindSymlink:
			target, err := s.Readlink(p)
			if err != nil {
				fmt.Fprintf(&b, "readlink %s: %v\n", p, err)
				continue
			}
			fmt.Fprintf(&b, "readlink %s: %s\n", p, target)
		}
	}
	for _, dir := range []string{"/", "/d"} {
		entries, err := s.ReadDir(dir)
		if err != nil {
			fmt.Fprintf(&b, "list %s: %v\n", dir, err)
			continue
		}
		for _, e := range entries {
			fmt.Fprintf(&b, "entry %s/%s: kind %d mode %o size %d\n",
				dir, e.Name, e.Attr.Kind, e.Attr.Mode, e.Attr.Size)
		}
	}
	return b.String()
}

// TestLayersAgreeOnOneOperationSequence protects TheoryOfUpperLayer: the three
// backends implement one contract, so one operation sequence must leave one
// observable state whatever backend holds the session's mutations. A divergence
// here is a defect in whichever layer disagrees, and the review reads a
// different answer depending on where the session recorded itself.
func TestLayersAgreeOnOneOperationSequence(t *testing.T) {
	stores := newLayerStores(t)
	big := bytes.Repeat([]byte("0123456789abcdef"), 1000)
	ops := []struct {
		name string
		run  func(s *Store) error
	}{
		{"write /a", func(s *Store) error { return s.WriteFile("/a", []byte("0123456789"), 0o644) }},
		{"splice into /a", func(s *Store) error { return s.SpliceFile("/a", 3, []byte("XYZ"), 0o644) }},
		{"shrink /a", func(s *Store) error { return s.TruncateFile("/a", 5, 0o644) }},
		{"grow /a", func(s *Store) error { return s.TruncateFile("/a", 20, 0o644) }},
		{"splice past the stored end", func(s *Store) error { return s.SpliceFile("/a", 18, []byte("PQ"), 0o644) }},
		{"write big /b", func(s *Store) error { return s.WriteFile("/b", big, 0o600) }},
		{"splice across a chunk", func(s *Store) error {
			return s.SpliceFile("/b", contentChunkSize-3, []byte("abcdef"), 0o600)
		}},
		{"shrink /b inside a chunk", func(s *Store) error {
			return s.TruncateFile("/b", contentChunkSize+5, 0o600)
		}},
		{"mkdir /d", func(s *Store) error { return s.Mkdir("/d", 0o700) }},
		{"write /d/f", func(s *Store) error { return s.WriteFile("/d/f", []byte("f"), 0o600) }},
		{"symlink /l", func(s *Store) error { return s.Symlink("a", "/l") }},
		{"rename /a to /c", func(s *Store) error { return s.Rename("/a", "/c") }},
		{"link /c to /h", func(s *Store) error { return s.Link("/c", "/h") }},
		{"remove /d/f", func(s *Store) error { return s.Remove("/d/f") }},
		{"chmod /b", func(s *Store) error { return s.SetMode("/b", 0o640) }},
	}
	paths := []string{"/", "/a", "/b", "/c", "/d", "/d/f", "/h", "/l"}
	for _, op := range ops {
		for name, s := range stores {
			if err := op.run(s); err != nil {
				t.Fatalf("%s on the %s layer: %v", op.name, name, err)
			}
		}
		views := make(map[string]string, len(stores))
		for name, s := range stores {
			views[name] = layerView(t, s, paths)
		}
		ref := views["memory"]
		for _, name := range []string{"sql", "dir"} {
			if views[name] != ref {
				t.Fatalf("after %q the %s and memory layers disagree:\n--- memory\n%s\n--- %s\n%s",
					op.name, name, ref, name, views[name])
			}
		}
	}
}
