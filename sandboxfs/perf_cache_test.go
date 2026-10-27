package sandboxfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"syscall"
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
	u := sqliteOf(t, store)
	if _, err := store.Stat("/a.txt"); err != nil {
		t.Fatal(err)
	}
	if !u.upperAbsent("/a.txt") {
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
	if u.upperAbsent("/a.txt") {
		t.Fatal("write left a cached absence in place")
	}
}

// TestBaseOpenFileMatchesRootWalk checks the descriptor open against the
// os.Root walk the read handle used before: a regular file opens with its
// bytes, and a directory, a symbolic link, and a missing path report the errors
// the read path answers, so a node without readable content is never opened and
// a pipe never blocks. See TheoryOfBaseFdAccess.
func TestBaseOpenFileMatchesRootWalk(t *testing.T) {
	store, base := newCacheTestStore(t)
	if store.baseFd < 0 {
		t.Skip("the platform offers no base descriptor")
	}
	if err := os.Mkdir(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	f, ok, err := baseOpenFile(store.baseFd, "a.txt")
	if !ok || err != nil {
		t.Fatalf("baseOpenFile: ok = %v, err = %v", ok, err)
	}
	got, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("content = %q, want %q", got, "hello")
	}
	if _, ok, err := baseOpenFile(store.baseFd, "d"); !ok || !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("directory open: ok = %v, err = %v, want EISDIR", ok, err)
	}
	if _, ok, err := baseOpenFile(store.baseFd, "link"); !ok || !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("link open: ok = %v, err = %v, want EINVAL", ok, err)
	}
	if _, ok, err := baseOpenFile(store.baseFd, "missing"); !ok || !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("missing open: ok = %v, err = %v, want ENOENT", ok, err)
	}
}

// TestBaseReadFileMatchesRootWalk checks the descriptor read against the
// os.Root read the store used before: a regular file yields the same bytes,
// and a directory, a symbolic link, and a missing path report the errors the
// store's read API answers, so a node without readable content is never read.
// See TheoryOfBaseFdAccess.
func TestBaseReadFileMatchesRootWalk(t *testing.T) {
	store, base := newCacheTestStore(t)
	if store.baseFd < 0 {
		t.Skip("the platform offers no base descriptor")
	}
	if err := os.Mkdir(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	got, ok, err := baseReadFile(store.baseFd, "a.txt")
	if !ok || err != nil {
		t.Fatalf("baseReadFile: ok = %v, err = %v", ok, err)
	}
	want, err := store.baseRoot.ReadFile("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fast content = %q, want %q", got, want)
	}
	if _, ok, err := baseReadFile(store.baseFd, "d"); !ok || !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("directory read: ok = %v, err = %v, want EISDIR", ok, err)
	}
	if _, ok, err := baseReadFile(store.baseFd, "link"); !ok || !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("link read: ok = %v, err = %v, want EINVAL", ok, err)
	}
	if _, ok, err := baseReadFile(store.baseFd, "missing"); !ok || !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("missing read: ok = %v, err = %v, want ENOENT", ok, err)
	}
}

// TestBaseHandleFileFallbackChecksTheKind protects TheoryOfBaseFdAccess on the
// path a platform without the base descriptor takes: the portable walk still
// refuses a directory and a symbolic link before it opens them, so a read
// handle never serves a directory's bytes and never follows a link. The test
// keeps off a pipe, because the defect it guards against would block rather
// than fail. See baseHandleFile.
func TestBaseHandleFileFallbackChecksTheKind(t *testing.T) {
	store, base := newCacheTestStore(t)
	if err := os.Mkdir(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	// A negative descriptor sends every base open through the portable walk,
	// the path a platform without the descriptor takes. The real descriptor is
	// closed first, so the test leaks none.
	closeBaseDirFd(store.baseFd)
	store.baseFd = -1
	server := newFuseServer(store, -1)
	if _, err := server.baseHandleFile(&fuseHandle{path: "/d"}); !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("directory handle: err = %v, want EISDIR", err)
	}
	if _, err := server.baseHandleFile(&fuseHandle{path: "/link"}); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("link handle: err = %v, want EINVAL", err)
	}
	f, err := server.baseHandleFile(&fuseHandle{path: "/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("content = %q, want %q", got, "hello")
	}
}

// TestChildPathMatchesPathJoin checks that naming one directory entry by
// appending to its canonical parent yields the path a LOOKUP of the same name
// stores: the reply's d_ino and the path's st_ino agree only if both name one
// path. "." and ".." resolve to the parent itself and its own parent, the
// values path.Join gives. See TheoryOfFuseMount.
func TestChildPathMatchesPathJoin(t *testing.T) {
	for _, parent := range []string{"/", "/a", "/a/b", "/a/b/c"} {
		for _, name := range []string{".", "..", "x", "main.go", "v2", "a.b-c_d"} {
			got := childPath(parent, name)
			want := path.Join(parent, name)
			if got != want {
				t.Errorf("childPath(%q, %q) = %q, want %q", parent, name, got, want)
			}
		}
	}
}

// TestCanonicalRelMatchesRelPath checks that the canonical-form helper the
// read path uses agrees with the general form on every canonical path, so the
// one-clean rule never changes which base path a request reads.
// See TheoryOfBaseFdAccess.
func TestCanonicalRelMatchesRelPath(t *testing.T) {
	for _, p := range []string{"/", "/a", "/a/b", "/a/b.c", "/deep/er/tree/leaf.go"} {
		q := Canonical(p)
		if got, want := canonicalRel(q), relPath(q); got != want {
			t.Fatalf("canonicalRel(%q) = %q, want %q", q, got, want)
		}
	}
}

// TestParentOfMatchesPathDir checks that the allocation-free parent of a
// canonical path agrees with path.Dir on every shape a sandbox path takes:
// the root, a top-level entry, and a nested path — including the paths
// childPath resolves for a listing's "..".
// See TheoryOfFuseMount.
func TestParentOfMatchesPathDir(t *testing.T) {
	for _, p := range []string{"/", "/a", "/a/b", "/a/b/c", "/deep/path/to/leaf.go"} {
		if got, want := parentOf(p), path.Dir(p); got != want {
			t.Fatalf("parentOf(%q) = %q, want %q", p, got, want)
		}
	}
}

// TestReadRangeServesWrittenWindowAfterCachedAbsence checks the read path: a
// window read recorded the absence, and the write that follows makes the upper
// layer answer the same window. See TheoryOfUpperAbsentCache.
func TestReadRangeServesWrittenWindowAfterCachedAbsence(t *testing.T) {
	store, _ := newCacheTestStore(t)
	u := sqliteOf(t, store)
	chunk, err := store.ReadRange("/a.txt", 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if string(chunk) != "hello" {
		t.Fatalf("chunk = %q, want %q", chunk, "hello")
	}
	if !u.upperAbsent("/a.txt") {
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

// TestFreshStoreReadsWithoutItsUpperLayer protects TheoryOfUpperAbsentCache: a
// session that has written nothing answers every lookup from the base, so its
// read path never reaches sqlite. The closed database states that fact — a
// lookup that asked it would fail — and the answers must be the base's own.
func TestFreshStoreReadsWithoutItsUpperLayer(t *testing.T) {
	store, base := newCacheTestStore(t)
	if err := os.Mkdir(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := sqliteOf(t, store)
	if !u.empty.Load() {
		t.Fatal("a store opened on an empty database did not record the empty upper layer")
	}
	if err := u.db.Close(); err != nil {
		t.Fatal(err)
	}
	a, err := store.Stat("/a.txt")
	if err != nil {
		t.Fatalf("stat of a base path with an empty upper layer: %v", err)
	}
	if a.Size != int64(len("hello")) {
		t.Fatalf("size = %d, want %d", a.Size, len("hello"))
	}
	content, err := store.ReadRange("/a.txt", 0, 5)
	if err != nil {
		t.Fatalf("read of a base path with an empty upper layer: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("content = %q, want %q", content, "hello")
	}
	entries, err := store.ReadDir("/")
	if err != nil {
		t.Fatalf("listing with an empty upper layer: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("listing holds %d entries, want a.txt and d", len(entries))
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
