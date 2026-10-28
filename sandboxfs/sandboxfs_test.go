package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	base := t.TempDir()
	s, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s, base
}

// TestHashBaseFileMatchesContentHash protects TheoryOfBaseCapture: the
// streaming digest and the in-memory digest must agree, because the apply
// step compares the host's whole-file hash with the recorded original. Two
// shapes of one hash would report every unchanged file as a conflict.
func TestHashBaseFileMatchesContentHash(t *testing.T) {
	base := t.TempDir()
	content := make([]byte, 300<<10)
	for i := range content {
		content[i] = byte(i*7 + i/251)
	}
	if err := os.WriteFile(filepath.Join(base, "f.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	got, err := hashBaseFile(root, "f.bin")
	if err != nil {
		t.Fatal(err)
	}
	if want := contentHash(content); got != want {
		t.Fatalf("streaming digest %s, in-memory digest %s", got, want)
	}
}

func TestFuseReadCachesBaseReaderAcrossWindows(t *testing.T) {
	store, base := newTestStore(t)
	if err := os.WriteFile(filepath.Join(base, "f.txt"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newFuseServer(store, -1)
	h := &fuseHandle{path: "/f.txt"}
	// The handle must be registered: invalidateHandles walks the server's
	// open-handle table, and an unregistered handle is never reached.
	s.handles[1] = h
	first, err := s.readFrom(h, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != "0123" {
		t.Fatalf("first window = %q", first)
	}
	if h.base == nil {
		t.Fatal("base reader not cached")
	}
	second, err := s.readFrom(h, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != "4567" {
		t.Fatalf("second window = %q", second)
	}
	// A session write must win over the cached base reader.
	if err := store.WriteFile("/f.txt", []byte("ABCDEFGHIJ"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := s.readFrom(h, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(third) != "ABCD" {
		t.Fatalf("window after write = %q", third)
	}
	// An invalidation marks the cached reader stale instead of closing it: the
	// invalidator may be another handle's writer, and a writer that waited for
	// this handle's lock would deadlock with a writer going the other way.
	// See TheoryOfFuseConcurrency.
	s.invalidateHandles("/f.txt")
	if !h.baseStale.Load() {
		t.Fatal("the invalidation left no stale mark")
	}
	// The mark is consumed by the handle's next base read, which closes the
	// descriptor that fixed the old view. The host path is deleted and made
	// again so the new content lives in a fresh inode: overwriting the file
	// would keep the same inode and the stale descriptor would read the new
	// bytes, proving nothing.
	if err := os.WriteFile(filepath.Join(base, "g.txt"), []byte("old!"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &fuseHandle{path: "/g.txt"}
	s.handles[2] = g
	if _, err := s.readFrom(g, 0, 4); err != nil {
		t.Fatal(err)
	}
	if g.base == nil {
		t.Fatal("base reader not cached")
	}
	if err := os.Remove(filepath.Join(base, "g.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "g.txt"), []byte("new!"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.invalidateHandles("/g.txt")
	if !g.baseStale.Load() {
		t.Fatal("the invalidation left no stale mark")
	}
	fresh, err := s.readFrom(g, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(fresh) != "new!" {
		t.Fatalf("window after invalidation = %q, want the host's current bytes", fresh)
	}
	if g.baseStale.Load() {
		t.Fatal("the stale mark survived the read that consumed it")
	}
	// Non-regular base paths are refused without being opened.
	if err := os.Symlink("target", filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	linkHandle := &fuseHandle{path: "/link"}
	if _, err := s.readFrom(linkHandle, 0, 4); err == nil {
		t.Fatal("link read was served")
	}
	if linkHandle.base != nil {
		t.Fatal("link was opened")
	}
	if err := os.Mkdir(filepath.Join(base, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readFrom(&fuseHandle{path: "/dir"}, 0, 4); err == nil {
		t.Fatal("directory read was served")
	}
}

func TestReadRangeReadsUpperLayerWindow(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.WriteFile("/f.txt", []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadRange("/f.txt", 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "2345" {
		t.Fatalf("window = %q", got)
	}
	got, err = store.ReadRange("/f.txt", 20, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("window past end = %q", got)
	}
	if err := store.Remove("/f.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadRange("/f.txt", 0, 4); err != ErrNotExist {
		t.Fatalf("read of a whiteout = %v", err)
	}
}

// TestReadRangeBoundsTheWindow protects TheoryOfCompletePOSIX: a window size is
// bounded before it becomes an allocation, so a caller cannot make the store
// reserve memory for bytes that cannot exist. Both layers are covered, because
// the bound is checked before either one is consulted.
func TestReadRangeBoundsTheWindow(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "f", "0123456789")
	if _, err := store.ReadRange("/f", 0, maxFileSize+1); !errors.Is(err, syscall.EFBIG) {
		t.Errorf("base window above the bound: got %v, want EFBIG", err)
	}
	if err := store.WriteFile("/g", []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadRange("/g", 0, maxFileSize+1); !errors.Is(err, syscall.EFBIG) {
		t.Errorf("upper window above the bound: got %v, want EFBIG", err)
	}
	got, err := store.ReadRange("/g", 2, 4)
	if err != nil || string(got) != "2345" {
		t.Errorf("window at the bound: %q %v, want 2345", got, err)
	}
	if got, err := store.ReadRange("/f", 2, 4); err != nil || string(got) != "2345" {
		t.Errorf("base window at the bound: %q %v, want 2345", got, err)
	}
}

// TestStatReportsSizeWithoutLoadingContent protects TheoryOfReadPath and
// TheoryOfChunkedContent: a metadata read carries no content. Stat reports a
// large file's size from the row's size column, and the mutation path's row
// read returns the same metadata without assembling the chunks the write is
// about to replace.
func TestStatReportsSizeWithoutLoadingContent(t *testing.T) {
	store, _ := newTestStore(t)
	content := make([]byte, 1<<16)
	for i := range content {
		content[i] = 'x'
	}
	if err := store.WriteFile("/big.txt", content, 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := store.Stat("/big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if a.Size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", a.Size, len(content))
	}
	r, ok, err := store.row("/big.txt")
	if err != nil || !ok {
		t.Fatalf("row: ok = %v, err = %v", ok, err)
	}
	if r.size != int64(len(content)) || r.kind != KindRegular || r.mode != 0o644 {
		t.Fatalf("row = %+v, want the recorded metadata", r)
	}
	if err := store.Symlink("target-name", "/link"); err != nil {
		t.Fatal(err)
	}
	la, err := store.Stat("/link")
	if err != nil {
		t.Fatal(err)
	}
	if la.Kind != KindSymlink || la.Size != int64(len("target-name")) {
		t.Fatalf("link attr = %+v", la)
	}
}

func writeHost(t *testing.T, base, name, content string) {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBaseCaptureIsBounded protects TheoryOfBaseCapture: a base file above the
// store's bound cannot become a stored row, so its first write reports EFBIG
// instead of materialising bytes the store must refuse. The refusal is the
// point: an unbounded materialisation holds the store's one connection and the
// server's one goroutine for as long as the file is large, and every other
// process's file I/O — a network client's certificate read among them — waits
// behind it past its own deadline.
func TestBaseCaptureIsBounded(t *testing.T) {
	store, base := newTestStore(t)
	p := filepath.Join(base, "big.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxFileSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/big.bin", 0, []byte("x"), 0o644); !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("first write into an oversized base file: err %v, want EFBIG", err)
	}
	// The refusal is a refusal to store, not a refusal to serve: the path stays
	// readable at its own size.
	a, err := store.Stat("/big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if a.Size != maxFileSize+1 {
		t.Fatalf("size %d, want %d", a.Size, maxFileSize+1)
	}
}

// TestBaseCaptureOfAnOversizedFile protects TheoryOfBaseCapture: the capture
// hashes at most the store's bound, so a request never spends a file's size in
// hash time however large the file is. The apply reads the host through the
// same bound, so a deletion still applies without a conflict.
func TestBaseCaptureOfAnOversizedFile(t *testing.T) {
	store, base := newTestStore(t)
	p := filepath.Join(base, "big.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("head")); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxFileSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("/big.bin"); err != nil {
		t.Fatalf("remove an oversized base file: %v", err)
	}
	r, ok, err := store.row("/big.bin")
	if err != nil || !ok {
		t.Fatalf("row after the remove: ok=%v err=%v", ok, err)
	}
	prefix := make([]byte, maxFileSize)
	copy(prefix, "head")
	if r.originalHash != contentHash(prefix) {
		t.Fatal("the capture did not stop at the store's bound")
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != ChangeDeleted || changes[0].Conflict != "" {
		t.Fatalf("changes %+v, want one conflict-free deletion", changes)
	}
	if _, err := store.Apply(func(Change) bool { return true }); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("the deletion did not reach the host: %v", err)
	}
}

// TestMetadataChangeOnALargeBaseFileStaysMetadataOnly protects
// TheoryOfBaseCapture: a chmod of a base file above the store's bound is a
// metadata change, so it costs one Lstat and never the file's content. Before
// the split the chmod materialised the whole file into chunks, an allocation
// the store could never hold, and answered EFBIG for a permission bit.
func TestMetadataChangeOnALargeBaseFileStaysMetadataOnly(t *testing.T) {
	store, base := newTestStore(t)
	p := filepath.Join(base, "big.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxFileSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMode("/big.bin", 0o600); err != nil {
		t.Fatalf("chmod an oversized base file: %v", err)
	}
	a, err := store.Stat("/big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if a.Size != maxFileSize+1 {
		t.Errorf("size %d, want %d", a.Size, maxFileSize+1)
	}
	if a.Mode.Perm() != 0o600 {
		t.Errorf("mode %o, want 600", a.Mode.Perm())
	}
	r, ok, err := store.row("/big.bin")
	if err != nil || !ok {
		t.Fatalf("row after the chmod: ok=%v err=%v", ok, err)
	}
	if r.size >= 0 {
		t.Errorf("row size %d, want the metadata-only sentinel", r.size)
	}
	entries, err := store.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "big.bin" && e.Attr.Size != maxFileSize+1 {
			t.Errorf("listing size %d, want %d", e.Attr.Size, maxFileSize+1)
		}
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != ChangeModified || changes[0].Conflict != "" {
		t.Fatalf("changes %+v, want one conflict-free mode-only change", changes)
	}
	if _, err := store.Apply(func(Change) bool { return true }); err != nil {
		t.Fatalf("apply: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("host mode %o, want 600", info.Mode().Perm())
	}
	if info.Size() != maxFileSize+1 {
		t.Errorf("host size %d, want %d", info.Size(), maxFileSize+1)
	}
}

// TestSpliceOnAMetadataOnlyRowMaterialisesTheBaseContent protects
// TheoryOfBaseCapture: a partial write into a metadata-only row first
// materialises the base's bytes, so the row's chunk set is the whole content
// and the payload never overwrites an empty file. A metadata change of any
// other kind keeps the row metadata-only, and the read falls through to the
// base.
func TestSpliceOnAMetadataOnlyRowMaterialisesTheBaseContent(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "f", "hello")
	if err := store.SetMode("/f", 0o600); err != nil {
		t.Fatal(err)
	}
	want := time.Unix(1_000_000_000, 0)
	if err := store.SetTimes("/f", want); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadFile("/f")
	if err != nil || string(got) != "hello" {
		t.Fatalf("read after a metadata change: %q %v", got, err)
	}
	if a, err := store.Stat("/f"); err != nil {
		t.Fatal(err)
	} else if a.Size != 5 || a.Mode.Perm() != 0o600 || !a.ModTime.Equal(want) {
		t.Fatalf("attr after a metadata change: size %d mode %o mtime %v", a.Size, a.Mode.Perm(), a.ModTime)
	}
	if err := store.SpliceFile("/f", 0, []byte("HE"), 0o600); err != nil {
		t.Fatalf("splice into a metadata-only row: %v", err)
	}
	got, err = store.ReadFile("/f")
	if err != nil || string(got) != "HEllo" {
		t.Fatalf("read after the splice: %q %v", got, err)
	}
}

// TestChangesOnAMetadataOnlyRowWhoseHostFileVanished protects
// TheoryOfBaseCapture and TheoryOfApplyConflict: a metadata-only row keeps its
// content in the base, so a host file deleted after the capture leaves nothing
// to read. The difference must surface as a conflict the user resolves, never
// as an error that fails the whole review, and the apply must skip it.
func TestChangesOnAMetadataOnlyRowWhoseHostFileVanished(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "f", "hello")
	if err := store.SetMode("/f", 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(base, "f")); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatalf("changes after the host file vanished: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes %+v, want one change", changes)
	}
	if changes[0].Conflict == "" {
		t.Fatalf("change %+v carries no conflict", changes[0])
	}
	if applied, err := store.Apply(func(Change) bool { return true }); err != nil || len(applied) != 0 {
		t.Fatalf("apply: applied %d, err %v; a conflicted change must be skipped", len(applied), err)
	}
}

func hostContent(t *testing.T, base, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func findChange(t *testing.T, changes []Change, p string) Change {
	t.Helper()
	for _, c := range changes {
		if c.Path == p {
			return c
		}
	}
	t.Fatalf("no change for %q in %+v", p, changes)
	return Change{}
}

func TestReadThroughAndAuditOnce(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "hello")
	for range 3 {
		got, err := s.ReadFile("/a.txt")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "hello" {
			t.Fatalf("got %q", got)
		}
	}
	ops, err := s.Ops()
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, op := range ops {
		if op.Op == "read" && op.Path == "/a.txt" {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("read recorded %d times, want 1", reads)
	}
}

func TestWriteOverridesBase(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "old")
	if err := s.WriteFile("/a.txt", []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadFile("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("got %q", got)
	}
	if hostContent(t, base, "a.txt") != "old" {
		t.Fatal("the host must stay unchanged before apply")
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	c := findChange(t, changes, "/a.txt")
	if c.Kind != ChangeModified {
		t.Fatalf("kind %q", c.Kind)
	}
	if string(c.Original) != "old" || string(c.Current) != "new" {
		t.Fatalf("change %+v", c)
	}
	if c.Conflict != "" {
		t.Fatalf("conflict %q", c.Conflict)
	}
}

func TestWhiteoutHidesBaseFile(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "old")
	if err := s.Remove("/a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat("/a.txt"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("err %v", err)
	}
	entries, err := s.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "a.txt" {
			t.Fatal("deleted file still listed")
		}
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if findChange(t, changes, "/a.txt").Kind != ChangeDeleted {
		t.Fatal("want a deleted change")
	}
}

func TestCreateListAndApply(t *testing.T) {
	s, base := newTestStore(t)
	if err := s.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("/d/f.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ReadDir("/d")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "f.txt" {
		t.Fatalf("entries %+v", entries)
	}
	applied, err := s.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied %+v", applied)
	}
	if hostContent(t, base, "d/f.txt") != "x" {
		t.Fatal("apply did not write the host")
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes after apply: %+v", changes)
	}
}

func TestNetChangeIsHashBased(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "same")
	if err := s.WriteFile("/a.txt", []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes %+v", changes)
	}
}

func TestCreateThenDeleteLeavesNoChange(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.WriteFile("/x.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("/x.txt"); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes %+v", changes)
	}
}

func TestApplyRejectsExternalChange(t *testing.T) {
	t.Run("external overwrite", func(t *testing.T) {
		s, base := newTestStore(t)
		writeHost(t, base, "a.txt", "old")
		if err := s.WriteFile("/a.txt", []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeHost(t, base, "a.txt", "outside")
		changes, err := s.Changes()
		if err != nil {
			t.Fatal(err)
		}
		if findChange(t, changes, "/a.txt").Conflict == "" {
			t.Fatal("want a conflict")
		}
		applied, err := s.Apply(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(applied) != 0 {
			t.Fatalf("applied %+v", applied)
		}
		if hostContent(t, base, "a.txt") != "outside" {
			t.Fatal("a conflicting apply must not touch the host")
		}
	})
	t.Run("external create", func(t *testing.T) {
		s, base := newTestStore(t)
		if err := s.WriteFile("/new.txt", []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeHost(t, base, "new.txt", "outside")
		changes, err := s.Changes()
		if err != nil {
			t.Fatal(err)
		}
		if findChange(t, changes, "/new.txt").Conflict == "" {
			t.Fatal("want a conflict")
		}
		applied, err := s.Apply(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(applied) != 0 {
			t.Fatalf("applied %+v", applied)
		}
		if hostContent(t, base, "new.txt") != "outside" {
			t.Fatal("a conflicting apply must not touch the host")
		}
	})
}

// TestApplyAllContinuesAfterAFailingChange pins the apply's fault isolation:
// one path the host refuses must not block the other changes of the same
// loop, or a single refused path would keep every change of the loop out of
// the host while the session still reads its own writes back. The refused
// deletion is a case no permission bit produces, so the test runs wherever
// the suite runs, root included: the host refilled a directory the session
// deleted, and the apply cannot remove it. See TheoryOfApplyFaultIsolation.
func TestApplyAllContinuesAfterAFailingChange(t *testing.T) {
	base := t.TempDir()
	record := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "gone"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte("old "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	store, err := OpenDirWithPolicy(record, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Remove("gone"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b.txt", "c.txt"} {
		if err := store.WriteFile(name, []byte("new "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The host refills the directory the session deleted, so the deletion
	// cannot apply: the host refuses to remove a directory that holds an
	// entry. The other changes of the loop must still reach the host.
	if err := os.WriteFile(filepath.Join(base, "gone", "y"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	applied, conflicts, err := store.ApplyAll()
	if err == nil {
		t.Fatal("expected the refused deletion to fail, got no error")
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %v", conflicts)
	}
	if len(applied) != 2 {
		t.Fatalf("got %d applied change(s), want 2: %v", len(applied), applied)
	}
	for _, name := range []string{"b.txt", "c.txt"} {
		got, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		if want := "new " + name; string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "gone", "y")); err != nil {
		t.Errorf("the host's refilled directory lost its entry: %v", err)
	}
}

func TestRemoveNonEmptyDir(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "d/a.txt", "x")
	if err := s.Remove("/d"); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("err %v", err)
	}
}

func TestRenameMovesContent(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "x")
	if err := s.Rename("/a.txt", "/b.txt"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadFile("/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "x" {
		t.Fatalf("got %q", got)
	}
	if _, err := s.Stat("/a.txt"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("err %v", err)
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if findChange(t, changes, "/b.txt").Kind != ChangeAdded {
		t.Fatal("want an added change at the destination")
	}
	if findChange(t, changes, "/a.txt").Kind != ChangeDeleted {
		t.Fatal("want a deleted change at the source")
	}
}

// TestRenameSamePathReportsMissingSource protects TheoryOfCompletePOSIX:
// rename(2) resolves the source before it treats the no-op, so renaming a
// missing path to itself reports ENOENT whatever the two names share. An
// existing path renamed to itself stays a successful no-op.
func TestRenameSamePathReportsMissingSource(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "x")
	if err := s.Rename("/missing", "/missing"); !errors.Is(err, ErrNotExist) {
		t.Errorf("rename of a missing source to itself: got %v, want ENOENT", err)
	}
	if err := s.Rename("/a.txt", "/a.txt"); err != nil {
		t.Errorf("rename of a base source to itself: got %v, want success", err)
	}
	if err := s.WriteFile("/new", []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("/new", "/new"); err != nil {
		t.Errorf("rename of an upper source to itself: got %v, want success", err)
	}
	if _, err := s.Stat("/missing"); !errors.Is(err, ErrNotExist) {
		t.Errorf("the refused rename left a row: %v", err)
	}
	if got, err := s.ReadFile("/a.txt"); err != nil || string(got) != "x" {
		t.Errorf("the no-op rename changed the source: %q %v", got, err)
	}
	if got, err := s.ReadFile("/new"); err != nil || string(got) != "y" {
		t.Errorf("the upper no-op rename changed the source: %q %v", got, err)
	}
}

func TestSymlinkRoundTrip(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Symlink("target.txt", "/link"); err != nil {
		t.Fatal(err)
	}
	target, err := s.Readlink("/link")
	if err != nil {
		t.Fatal(err)
	}
	if target != "target.txt" {
		t.Fatalf("target %q", target)
	}
	a, err := s.Stat("/link")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindSymlink {
		t.Fatalf("kind %v", a.Kind)
	}
}

// TestSymlinkRefusesAnEmptyTarget protects TheoryOfCompletePOSIX: a link names
// a target, and an empty target names nothing. Linux refuses symlink(2) with
// ENOENT, and a store that recorded the empty link would hand the apply a
// change the host can never carry out.
func TestSymlinkRefusesAnEmptyTarget(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Symlink("", "/link"); !errors.Is(err, syscall.ENOENT) {
		t.Errorf("symlink with an empty target: got %v, want ENOENT", err)
	}
	if _, err := s.Stat("/link"); !errors.Is(err, ErrNotExist) {
		t.Errorf("the refused link left a node: %v", err)
	}
	if err := s.Symlink("target", "/link"); err != nil {
		t.Fatalf("a link with a target must succeed: %v", err)
	}
	if got, err := s.Readlink("/link"); err != nil || got != "target" {
		t.Errorf("link target %q (error %v), want target", got, err)
	}
}

func TestRenameDirectory(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "src/a.txt", "a")
	writeHost(t, base, "src/b/c.txt", "c")
	if err := s.WriteFile("/src/new.txt", []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("/src", "/dst"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat("/src"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("source survived rename: %v", err)
	}
	for p, want := range map[string]string{
		"/dst/a.txt":   "a",
		"/dst/b/c.txt": "c",
		"/dst/new.txt": "n",
	} {
		got, err := s.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", p, got, want)
		}
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range changes {
		seen[c.Path] = true
		switch c.Path {
		case "/src", "/src/a.txt", "/src/b", "/src/b/c.txt":
			if c.Kind != ChangeDeleted {
				t.Errorf("%s: want deleted, got %s", c.Path, c.Kind)
			}
		case "/dst", "/dst/a.txt", "/dst/b", "/dst/b/c.txt", "/dst/new.txt":
			if c.Kind != ChangeAdded {
				t.Errorf("%s: want added, got %s", c.Path, c.Kind)
			}
		}
	}
	for _, p := range []string{"/src", "/dst", "/dst/a.txt", "/dst/b/c.txt"} {
		if !seen[p] {
			t.Fatalf("change %s missing from %+v", p, changes)
		}
	}
}

func TestRenameDirectoryWithFIFO(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Mkdir("/src", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Mkfifo("/src/pipe", 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("/src/a.txt", []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("/src", "/dst"); err != nil {
		t.Fatal(err)
	}
	a, err := s.Stat("/dst/pipe")
	if err != nil {
		t.Fatalf("pipe missing after directory rename: %v", err)
	}
	if a.Kind != KindFIFO {
		t.Fatalf("dst/pipe kind %v, want KindFIFO", a.Kind)
	}
	if got, err := s.ReadFile("/dst/a.txt"); err != nil || string(got) != "a" {
		t.Fatalf("regular file after directory rename: %q %v", got, err)
	}
	if _, err := s.Stat("/src/pipe"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("source pipe survived the rename: %v", err)
	}
}

func TestRenameDirectoryReplacesEmptyDestination(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "src/a.txt", "a")
	if err := s.Mkdir("/dst", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("/src", "/dst"); err != nil {
		t.Fatalf("rename onto an empty directory: %v", err)
	}
	if got, err := s.ReadFile("/dst/a.txt"); err != nil || string(got) != "a" {
		t.Fatalf("content after rename: %q %v", got, err)
	}
	if _, err := s.Stat("/src"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("source survived the rename: %v", err)
	}
}

func TestRenameDirectoryReportsPOSIXErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, s *Store, base string) string
		want  error
	}{
		{
			name: "onto a populated destination",
			setup: func(t *testing.T, s *Store, base string) string {
				writeHost(t, base, "dst/b.txt", "b")
				return "/dst"
			},
			want: ErrNotEmpty,
		},
		{
			name: "onto a file",
			setup: func(t *testing.T, s *Store, base string) string {
				writeHost(t, base, "dst.txt", "f")
				return "/dst.txt"
			},
			want: syscall.ENOTDIR,
		},
		{
			name: "into itself",
			setup: func(t *testing.T, s *Store, base string) string {
				return "/src/inner"
			},
			want: syscall.EINVAL,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, base := newTestStore(t)
			writeHost(t, base, "src/a.txt", "a")
			dst := tc.setup(t, s, base)
			if err := s.Rename("/src", dst); !errors.Is(err, tc.want) {
				t.Fatalf("rename /src to %s: want %v, got %v", dst, tc.want, err)
			}
		})
	}
}

func TestLinkSharesContent(t *testing.T) {
	s, base := newTestStore(t)
	writeHost(t, base, "a.txt", "hello")
	if err := s.Link("/a.txt", "/b.txt"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadFile("/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range changes {
		if c.Path == "/b.txt" {
			found = true
			if c.Kind != ChangeAdded {
				t.Fatalf("want added, got %s", c.Kind)
			}
		}
		if c.Path == "/a.txt" {
			t.Fatalf("source must not change, got %s", c.Kind)
		}
	}
	if !found {
		t.Fatal("destination missing from changes")
	}
}

func TestCanWriteFollowsPolicy(t *testing.T) {
	base := t.TempDir()
	allow := func(p string) error {
		if p == "/allowed" {
			return nil
		}
		return errors.New("not allowed")
	}
	s, err := OpenWithPolicy(filepath.Join(t.TempDir(), "sandbox.db"), base, allow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.CanWrite("/allowed"); err != nil {
		t.Fatalf("allowed path: %v", err)
	}
	if err := s.CanWrite("/denied"); err == nil {
		t.Fatal("denied path must fail")
	}
}

// TestOwnDatabaseIsNeverASandboxTarget protects TheoryOfSandboxFilesystem: the
// store recognises its own record by name and by identity, filters it from the
// change list, and refuses to apply it. The stale row is injected through the
// sqlite layer's connection; a memory layer carries no record to protect, so
// the test names the sqlite one.
func TestOwnDatabaseIsNeverASandboxTarget(t *testing.T) {
	s, _ := newTestStore(t)
	u := sqliteOf(t, s)
	// The mount's base view is a bind of "/", so the sandbox path of the
	// database equals its recorded path. Simulate that shape and check the
	// three SQLite siblings by name.
	s.dbPath = "/app.db"
	for _, p := range []string{"/app.db", "/app.db-wal", "/app.db-shm"} {
		if !s.isOwnDBPath(p) {
			t.Fatalf("%s must be recognised as the store's own database", p)
		}
	}
	for _, p := range []string{"/app.go", "/app.db.bak", "/data/app.db"} {
		if s.isOwnDBPath(p) {
			t.Fatalf("%s must not be recognised as the store's own database", p)
		}
	}

	// A pre-existing row for the database is filtered from the change list
	// even though the write path now refuses future mutations.
	if _, err := u.db.Exec(
		`INSERT INTO files (path, kind, mode, mtime, deleted, original_exists, original_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"/app.db", 0, 0o644, 0, 0, 0, "",
	); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Path == "/app.db" {
			t.Fatalf("own database appeared as change: %+v", c)
		}
	}
	if err := s.applyOne(Change{Path: "/app.db", Kind: ChangeAdded}); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("apply of own database: %v", err)
	}
}

func TestWriteRefusesOwnDatabase(t *testing.T) {
	base := t.TempDir()
	dbPath := filepath.Join(base, "sandbox.db")
	s, err := Open(dbPath, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// The sandbox path of the database equals its recorded path when the
	// base view is a bind of "/": here the test constructs that shape by
	// naming the database under the base. The database file itself exists
	// in the base, so a refused write must be checked against the upper
	// layer's rows, not against Stat, which would succeed off the base.
	s.dbPath = "/sandbox.db"
	for _, p := range []string{"/sandbox.db", "/sandbox.db-wal", "/sandbox.db-shm"} {
		err := s.WriteFile(p, []byte("x"), 0o644)
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("write to %s: want EACCES, got %v", p, err)
		}
		if _, ok, err := s.row(p); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Fatalf("refused write leaked an upper row for %s", p)
		}
	}
	if err := s.WriteFile("/ok.txt", []byte("x"), 0o644); err != nil {
		t.Fatalf("unrelated write rejected: %v", err)
	}
}
