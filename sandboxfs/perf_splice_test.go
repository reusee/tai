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

// TestTruncateFileShrinksAndGrows checks the chunk-shaped truncation: a shrink
// keeps the prefix the new length retains and drops the chunks beyond it, and
// a grow extends the length with zeros without writing them. See
// TheoryOfChunkedContent.
func TestTruncateFileShrinksAndGrows(t *testing.T) {
	store := newSpliceTestStore(t)
	content := bytes.Repeat([]byte("abc"), 48<<10) // 144 KiB, three chunks
	if err := store.WriteFile("/t.bin", content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.TruncateFile("/t.bin", 100<<10, 0o644); err != nil {
		t.Fatal(err)
	}
	got := spliceTestContent(t, store, "/t.bin")
	if !bytes.Equal(got, content[:100<<10]) {
		t.Fatalf("shrunk content mismatch: %d bytes", len(got))
	}
	if err := store.TruncateFile("/t.bin", 120<<10, 0o644); err != nil {
		t.Fatal(err)
	}
	got = spliceTestContent(t, store, "/t.bin")
	if len(got) != 120<<10 {
		t.Fatalf("grown length = %d, want %d", len(got), 120<<10)
	}
	if !bytes.Equal(got[:100<<10], content[:100<<10]) {
		t.Fatal("grown file lost its prefix")
	}
	for _, b := range got[100<<10:] {
		if b != 0 {
			t.Fatal("grown tail is not zero-filled")
		}
	}
}

// TestSpliceFileAlignedChunk checks the chunk-aligned window: a payload that
// covers a chunk whole stores that payload, so the aligned windows a toolchain
// emits round-trip without a read of the stored chunk. See
// TheoryOfChunkedContent.
func TestSpliceFileAlignedChunk(t *testing.T) {
	store := newSpliceTestStore(t)
	if err := store.WriteFile("/c.bin", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("ab"), contentChunkSize/2)
	if err := store.SpliceFile("/c.bin", 0, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := spliceTestContent(t, store, "/c.bin"); !bytes.Equal(got, payload) {
		t.Fatalf("aligned chunk write stored %d bytes, want %d", len(got), len(payload))
	}
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
// reads as zeros, because no chunk covers it and a read assembles zeros for
// absent chunks, so the write stores only its own payload and no padding.
// See TheoryOfWriteSplice.
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
// empty file — the shape a create or a truncating open leaves behind. An empty
// file stores no chunk, so the write covers whole chunks from its own payload
// and no boundary chunk needs reading. See TheoryOfWriteSplice.
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

// TestMetaRowOmitsContent checks that the overwrite path's row read loads
// metadata only: content lives in the chunk table, and the write already holds
// the bytes it stores, so a read that assembled content would copy a file the
// write is about to discard. See TheoryOfChunkedContent.
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
	if r.size != int64(len(content)) {
		t.Fatalf("metaRow size = %d, want %d", r.size, len(content))
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
