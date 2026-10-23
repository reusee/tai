package sandboxfs

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// newCacheTestStore builds a store over a base directory holding one file, so
// a test can exercise the read path against real base content.
func newCacheTestStore(t *testing.T) (*Store, string) {
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
	return store, base
}

// TestUpperAbsentCacheRefreshesOnWrite checks that a path cached as absent
// serves its new upper-layer content once the session writes it: the write
// clears the entry, so the lookup reads the row instead of the cached absence.
// See TheoryOfUpperAbsentCache.
func TestUpperAbsentCacheRefreshesOnWrite(t *testing.T) {
	store, _ := newCacheTestStore(t)
	if _, err := store.Stat("/a.txt"); err != nil {
		t.Fatal(err)
	}
	if !store.upperAbsent("/a.txt") {
		t.Fatal("lookup of a base-only path did not record the absence")
	}
	if err := store.WriteFile("/a.txt", []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	content, err := store.ReadFile("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "changed" {
		t.Fatalf("content = %q, want %q", content, "changed")
	}
	if store.upperAbsent("/a.txt") {
		t.Fatal("write left a cached absence in place")
	}
}

// TestReadRangeServesWrittenWindowAfterCachedAbsence checks the read path: a
// window read recorded the absence, and the write that follows makes the upper
// layer answer the same window.
func TestReadRangeServesWrittenWindowAfterCachedAbsence(t *testing.T) {
	store, _ := newCacheTestStore(t)
	chunk, err := store.ReadRange("/a.txt", 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if string(chunk) != "hello" {
		t.Fatalf("chunk = %q, want %q", chunk, "hello")
	}
	if !store.upperAbsent("/a.txt") {
		t.Fatal("base read did not record the upper-layer absence")
	}
	if err := store.WriteFile("/a.txt", []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	chunk, err = store.ReadRange("/a.txt", 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if string(chunk) != "hello world" {
		t.Fatalf("chunk = %q, want %q", chunk, "hello world")
	}
}

// TestEncodeStructDirectPathMatchesReflection checks that the direct encoder
// and the reflection encoder agree byte for byte on every hot reply type: the
// kernel parses the reply by its own offsets, so a divergence is a protocol
// fault rather than a cosmetic one. See TheoryOfFuseMount.
func TestEncodeStructDirectPathMatchesReflection(t *testing.T) {
	attr := fuseAttr{
		Ino: 7, Size: 11, Blocks: 1,
		Atime: 100, Mtime: 200, Ctime: 300,
		Atimensec: 1, Mtimensec: 2, Ctimensec: 3,
		Mode: 0o100644, Nlink: 1, Uid: 1000, Gid: 1000,
		Blksize: 4096,
	}
	entry := fuseEntryOut{
		Nodeid: 9, Generation: 1, EntryValid: 5, AttrValid: 5,
		EntryValidNsec: 6, AttrValidNsec: 7, Attr: attr,
	}
	direct := encodeStruct(&entry)
	reflected := make([]byte, binarySizeOf(entry))
	if err := binaryWrite(entry, reflected); err != nil {
		t.Fatal(err)
	}
	if len(direct) != len(reflected) {
		t.Fatalf("entry size = %d, want %d", len(direct), len(reflected))
	}
	for i := range direct {
		if direct[i] != reflected[i] {
			t.Fatalf("entry byte %d = %#x, want %#x", i, direct[i], reflected[i])
		}
	}
	attrOut := fuseAttrOut{AttrValid: 5, AttrValidNsec: 6, Attr: attr}
	direct = encodeStruct(&attrOut)
	reflected = make([]byte, binarySizeOf(attrOut))
	if err := binaryWrite(attrOut, reflected); err != nil {
		t.Fatal(err)
	}
	if len(direct) != len(reflected) {
		t.Fatalf("attr size = %d, want %d", len(direct), len(reflected))
	}
	for i := range direct {
		if direct[i] != reflected[i] {
			t.Fatalf("attr byte %d = %#x, want %#x", i, direct[i], reflected[i])
		}
	}
}

func binarySizeOf(v any) int {
	return binary.Size(v)
}

func binaryWrite(v any, buf []byte) error {
	return binary.Write(bytes.NewBuffer(buf[:0]), binary.LittleEndian, v)
}

// BenchmarkStatBasePath measures the metadata lookup of a path the session
// never writes: the base answers it and the upper-layer absence is cached, so
// the cost is one canonicalization, one map read, and one base stat.
func BenchmarkStatBasePath(b *testing.B) {
	base := b.TempDir()
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("hello"), 0o644); err != nil {
		b.Fatal(err)
	}
	store, err := Open(filepath.Join(b.TempDir(), "sandbox.db"), base)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Stat("/a.txt"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadRangeBasePath measures one read window of a path the session
// never writes: the upper layer is asked once, then the base answers every
// window with a single positioned read.
func BenchmarkReadRangeBasePath(b *testing.B) {
	base := b.TempDir()
	content := make([]byte, 4096)
	if err := os.WriteFile(filepath.Join(base, "a.bin"), content, 0o644); err != nil {
		b.Fatal(err)
	}
	store, err := Open(filepath.Join(b.TempDir(), "sandbox.db"), base)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.ReadRange("/a.bin", 0, 4096); err != nil {
			b.Fatal(err)
		}
	}
}
