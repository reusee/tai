package sandboxfs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFreshListingIsTheBaseListing guards the empty-upper listing path: a
// session that has written nothing must read every base entry, sorted, with
// the kind of each one. The merged path is not exercised here; TestWhiteout
// and TestCreateListAndApply cover it. See TheoryOfReadPath and
// TheoryOfUpperAbsentCache.
func TestFreshListingIsTheBaseListing(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"c.txt", "a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, err := store.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.txt", "b.txt", "c.txt", "d"}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, name := range want {
		if entries[i].Name != name {
			t.Fatalf("entry %d = %q, want %q", i, entries[i].Name, name)
		}
	}
	if entries[3].Attr.Kind != KindDir {
		t.Fatalf("d kind = %v, want a directory", entries[3].Attr.Kind)
	}
}
