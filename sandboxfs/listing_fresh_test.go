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

// TestLiveUpperEntrySurvivesBaseEntryVanishingMidWindow protects the union
// rule that a live upper row replaces the base entry. The base name was already
// enumerated when the host entry vanished before its attribute read; that race
// must not hide the session's own file from the listing.
func TestLiveUpperEntrySurvivesBaseEntryVanishingMidWindow(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "f", "old")
	if err := store.WriteFile("/f", []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	cursor, err := store.openDir("/")
	if err != nil {
		t.Fatal(err)
	}
	defer cursor.Close()

	// mergeBase receives names returned by the base directory's enumeration.
	// Removing the host file now reproduces its disappearance between that
	// enumeration and the per-entry stat.
	if err := os.Remove(filepath.Join(base, "f")); err != nil {
		t.Fatal(err)
	}
	cursor.mergeBase("f")
	if len(cursor.pending) != 1 {
		t.Fatalf("the merged window holds %d entries, want the live upper entry", len(cursor.pending))
	}
	entry := cursor.pending[0]
	if entry.Name != "f" || entry.Attr.Kind != KindRegular || entry.Attr.Mode != 0o600 || entry.Attr.Size != 3 {
		t.Fatalf("merged entry = %+v, want the upper file's attributes", entry)
	}
}
