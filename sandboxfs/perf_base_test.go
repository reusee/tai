package sandboxfs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// newBaseFastStore builds a store whose base holds one regular file, one
// directory, and one symbolic link, so the fast base paths can be compared
// against the os.Root paths they replace. See TheoryOfBaseFdAccess.
func newBaseFastStore(t *testing.T) *Store {
	t.Helper()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestBaseStatMatchesRootWalk checks the fast stat against the os.Root walk the
// store used before: a file, a directory, a symbolic link, and a missing path
// must report the same kind, size, permissions, and time.
func TestBaseStatMatchesRootWalk(t *testing.T) {
	store := newBaseFastStore(t)
	for _, p := range []string{"/a.txt", "/d", "/link"} {
		fast, err := store.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		info, err := store.baseRoot.Lstat(relPath(p))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		want := attrOf(info)
		if fast.Kind != want.Kind || fast.Size != want.Size || fast.Mode != want.Mode {
			t.Fatalf("%s: fast = %+v, root = %+v", p, fast, want)
		}
		if !fast.ModTime.Equal(want.ModTime) {
			t.Fatalf("%s: mtime = %v, want %v", p, fast.ModTime, want.ModTime)
		}
	}
	if _, err := store.Stat("/missing"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("missing path error = %v, want ErrNotExist", err)
	}
}

// TestBaseReadRangeMatchesRootWalk checks the fast window read: a window of a
// base file serves its bytes, a directory reports EISDIR, and a link reports
// EINVAL, matching the os.Root path's answers.
func TestBaseReadRangeMatchesRootWalk(t *testing.T) {
	store := newBaseFastStore(t)
	chunk, err := store.ReadRange("/a.txt", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if string(chunk) != "ell" {
		t.Fatalf("window = %q, want %q", chunk, "ell")
	}
	if _, err := store.ReadRange("/d", 0, 8); !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("directory error = %v, want EISDIR", err)
	}
	if _, err := store.ReadRange("/link", 0, 8); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("link error = %v, want EINVAL", err)
	}
	if chunk, err := store.ReadRange("/a.txt", 1024, 8); err != nil || len(chunk) != 0 {
		t.Fatalf("window past the end = %q, %v; want empty", chunk, err)
	}
}

// TestReadFromReusesWindowBuffer checks that a streamed base file costs one
// buffer per handle: the second window read into the same handle reuses the
// allocation the first one grew. See TheoryOfReadPath.
func TestReadFromReusesWindowBuffer(t *testing.T) {
	store := newBaseFastStore(t)
	s := newFuseServer(store, -1)
	h := &fuseHandle{path: "/a.txt"}
	if _, err := s.readFrom(h, 0, 5); err != nil {
		t.Fatal(err)
	}
	if cap(h.readBuf) < 5 {
		t.Fatalf("read buffer = %d bytes, want at least 5", cap(h.readBuf))
	}
	first := &h.readBuf[0]
	if _, err := s.readFrom(h, 0, 5); err != nil {
		t.Fatal(err)
	}
	if &h.readBuf[0] != first {
		t.Fatal("second window reallocated the handle's read buffer")
	}
}

// BenchmarkReadFromBaseHandle measures one streamed window of a base file
// through the server: the read path a toolchain spends its time in.
func BenchmarkReadFromBaseHandle(b *testing.B) {
	base := b.TempDir()
	content := bytes.Repeat([]byte("x"), 64<<10)
	if err := os.WriteFile(filepath.Join(base, "a.bin"), content, 0o644); err != nil {
		b.Fatal(err)
	}
	store, err := Open(filepath.Join(b.TempDir(), "sandbox.db"), base)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	s := newFuseServer(store, -1)
	h := &fuseHandle{path: "/a.bin"}
	if _, err := s.readFrom(h, 0, 64<<10); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(64 << 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.readFrom(h, 0, 64<<10); err != nil {
			b.Fatal(err)
		}
	}
}

// TestHandleOpenKeepsPageCache checks that a read-only open hands the kernel
// permission to keep the file's pages across opens, so a repeated read of a
// dependency never reaches the filesystem again, while a write-mode open keeps
// its pages invalidatable. See TheoryOfOpenCaching.
func TestHandleOpenKeepsPageCache(t *testing.T) {
	store := newBaseFastStore(t)
	s := newFuseServer(store, -1)
	id, err := s.nodeOf("/a.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.handleOpen(id, encodeStruct(&fuseOpenIn{Flags: 0}))
	if err != nil {
		t.Fatal(err)
	}
	var out fuseOpenOut
	decodeStruct(&out, reply)
	if out.OpenFlags&fuseOpenKeepCache == 0 {
		t.Fatalf("read-only open flags = %#x, want FOPEN_KEEP_CACHE", out.OpenFlags)
	}
	reply, err = s.handleOpen(id, encodeStruct(&fuseOpenIn{Flags: uint32(unix.O_WRONLY)}))
	if err != nil {
		t.Fatal(err)
	}
	decodeStruct(&out, reply)
	if out.OpenFlags&fuseOpenKeepCache != 0 {
		t.Fatalf("write-mode open flags = %#x, want no FOPEN_KEEP_CACHE", out.OpenFlags)
	}
}
