package sandboxfs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// newSpliceTestStore builds a store whose base holds one regular file, so a
// test can exercise a splice against real base content.
func newSpliceTestStore(t *testing.T) *Store {
	t.Helper()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func spliceTestContent(t *testing.T, store *Store, p string) []byte {
	t.Helper()
	b, err := store.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestSpliceFileOverwritesAndAppends checks the two shapes of a delta that
// land inside or exactly at the stored end: an overwrite keeps the surrounding
// bytes and an append adds at the end. See TheoryOfWriteSplice.
func TestSpliceFileOverwritesAndAppends(t *testing.T) {
	store := newSpliceTestStore(t)
	if err := store.WriteFile("/a.txt", []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/a.txt", 6, []byte("WORLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := spliceTestContent(t, store, "/a.txt"), []byte("hello WORLD"); !bytes.Equal(got, want) {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if err := store.SpliceFile("/a.txt", 11, []byte("!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := spliceTestContent(t, store, "/a.txt"), []byte("hello WORLD!"); !bytes.Equal(got, want) {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestSpliceFilePadsGapWithZeros checks a write past the stored end: the gap
// is zeros, exactly as the handle's buffer pads it, so the stored blob and the
// buffer stay byte-identical. See TheoryOfWriteSplice.
func TestSpliceFilePadsGapWithZeros(t *testing.T) {
	store := newSpliceTestStore(t)
	if err := store.WriteFile("/a.txt", []byte("ab"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/a.txt", 5, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := []byte{'a', 'b', 0, 0, 0, 'z'}
	if got := spliceTestContent(t, store, "/a.txt"); !bytes.Equal(got, want) {
		t.Fatalf("content = %v, want %v", got, want)
	}
}

// TestSpliceFileCapturesBaseContent checks the first delta of a path the upper
// layer does not hold yet: the base content the write starts from is captured
// into the row, so a delta on top of a base file lands at the right offset.
func TestSpliceFileCapturesBaseContent(t *testing.T) {
	store := newSpliceTestStore(t)
	if err := store.SpliceFile("/a.txt", 1, []byte("EY"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := spliceTestContent(t, store, "/a.txt"), []byte("hEYlo"); !bytes.Equal(got, want) {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestSpliceFileIntoEmptyStoredContent checks the delta that lands on a stored
// empty file — the shape a create or a truncating open leaves behind. The prefix
// and suffix of an empty blob must contribute nothing, not swallow the
// concatenation: an empty fragment is not a blob, and a NULL operand would store
// nothing at all. See TheoryOfWriteSplice.
func TestSpliceFileIntoEmptyStoredContent(t *testing.T) {
	store := newSpliceTestStore(t)
	if err := store.WriteFile("/a.txt", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/a.txt", 0, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := spliceTestContent(t, store, "/a.txt"), []byte("hi"); !bytes.Equal(got, want) {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestSpliceFileRevivesDeletedPath checks the whiteout case: a path the session
// deleted holds no live regular row, so the guarded splice matches nothing and
// the retry stores the revived file starting from nothing. Resurrecting the
// deleted bytes would undo the deletion. See TheoryOfWriteSplice.
func TestSpliceFileRevivesDeletedPath(t *testing.T) {
	store := newSpliceTestStore(t)
	if err := store.Remove("/a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/a.txt", 0, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := spliceTestContent(t, store, "/a.txt"), []byte("hi"); !bytes.Equal(got, want) {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestMetaRowOmitsContent checks that the overwrite path's row read leaves the
// content blob in the database: the write already holds the bytes it stores, so
// loading the old blob would copy a file the write is about to discard.
// See TheoryOfWriteSplice.
func TestMetaRowOmitsContent(t *testing.T) {
	store := newSpliceTestStore(t)
	content := bytes.Repeat([]byte("x"), 1<<16)
	if err := store.WriteFile("/a.txt", content, 0o644); err != nil {
		t.Fatal(err)
	}
	r, ok, err := store.metaRow("/a.txt")
	if err != nil || !ok {
		t.Fatalf("metaRow: ok = %v, err = %v", ok, err)
	}
	if len(r.content) != 0 {
		t.Fatalf("metaRow loaded %d content bytes, want none", len(r.content))
	}
	if r.kind != KindRegular || r.mode != 0o644 {
		t.Fatalf("metaRow = %+v, want the kind and mode fields", r)
	}
}

// TestSpliceFileRejectsDirectory checks the type guard: a directory carries no
// writable content, so the delta is refused before any row changes.
func TestSpliceFileRejectsDirectory(t *testing.T) {
	store := newSpliceTestStore(t)
	if err := store.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/d", 0, []byte("x"), 0o644); err == nil {
		t.Fatal("splice into a directory was accepted")
	}
}
