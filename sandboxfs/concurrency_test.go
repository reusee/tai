package sandboxfs

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// newConcurrencyTestStore opens a store over an empty base directory, so a
// test starts with no upper row and no base entry.
func newConcurrencyTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "sandbox.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, base
}

// TestRememberAbsentRejectsAStaleEpoch protects TheoryOfUpperAbsentCache: a
// lookup that found no row stamps itself with the write epoch before it
// queries, so a write that commits in between keeps the absence from being
// recorded. Without the stamp the cache hides the committed row for the rest
// of the session and the read keeps serving the base.
func TestRememberAbsentRejectsAStaleEpoch(t *testing.T) {
	store, _ := newConcurrencyTestStore(t)
	u := sqliteOf(t, store)
	epoch := u.epoch.Load()
	if err := store.WriteFile("/a.txt", []byte("written"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The lookup's own query ran before the write above; its record runs now.
	u.rememberAbsent("/a.txt", epoch)
	if u.upperAbsent("/a.txt") {
		t.Fatal("a stale absence survived the write that committed the row")
	}
	content, err := store.ReadFile("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "written" {
		t.Fatalf("content = %q, want %q", content, "written")
	}
}

// TestConcurrentWriteIsNeverHiddenByACachedAbsence runs the interleaving the
// cache must survive: one goroutine looks a fresh path up while another writes
// it. Whatever order the two reach, the absence may not outlive the write, so
// the path serves the bytes the write stored.
// See TheoryOfUpperAbsentCache.
func TestConcurrentWriteIsNeverHiddenByACachedAbsence(t *testing.T) {
	store, _ := newConcurrencyTestStore(t)
	u := sqliteOf(t, store)
	for i := 0; i < 200; i++ {
		p := fmt.Sprintf("/f%d.txt", i)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = store.Stat(p)
		}()
		go func() {
			defer wg.Done()
			if err := store.WriteFile(p, []byte("written"), 0o644); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		if u.upperAbsent(p) {
			t.Fatalf("%s: a cached absence outlived the write that committed the row", p)
		}
		content, err := store.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "written" {
			t.Fatalf("%s: content = %q, want %q", p, content, "written")
		}
	}
}

// TestMaterializeContentKeepsAConcurrentWritersRow protects
// TheoryOfWriteSplice: the first write of a fresh path materialises the row,
// and another writer can create that row first. The row it created is the
// content the delta lands on; replacing it drops the bytes that writer stored.
func TestMaterializeContentKeepsAConcurrentWritersRow(t *testing.T) {
	store, _ := newConcurrencyTestStore(t)
	if _, ok, err := store.row("/x.txt"); err != nil || ok {
		t.Fatalf("row = %v, %v; want no row", ok, err)
	}
	if err := store.WriteFile("/x.txt", []byte("the other writer"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The interleaving: this writer found no live regular row before the write
	// above and materialises after it.
	if _, err := store.materializeContent("/x.txt", -1, 0o644); err != nil {
		t.Fatal(err)
	}
	content, err := store.ReadFile("/x.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "the other writer" {
		t.Fatalf("content = %q, want the bytes the other writer stored", content)
	}
}
