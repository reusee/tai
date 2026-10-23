package sandboxfs

import (
	"bytes"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBaseFastPathEngaged checks that the read-through base descriptor is open
// on Linux. The stat and read fast paths rest on it, and a silent fallback to
// the os.Root walk would leave every benchmark measuring the path the change
// was meant to replace. See TheoryOfBaseFdAccess.
func TestBaseFastPathEngaged(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the base descriptor fast path is Linux-only")
	}
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.baseFd < 0 {
		t.Fatal("base directory descriptor is not open; the fast path is inactive")
	}
}

// newWriteBenchServer builds a server over an empty base with one writable
// handle, so a benchmark measures the write path alone.
func newWriteBenchServer(b *testing.B) (*fuseServer, *fuseHandle) {
	b.Helper()
	store, err := Open(filepath.Join(b.TempDir(), "sandbox.db"), b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	if err := store.WriteFile("/w.bin", nil, 0o644); err != nil {
		b.Fatal(err)
	}
	s := newFuseServer(store, -1)
	h := &fuseHandle{path: "/w.bin", mode: 0o644}
	s.handles[1] = h
	return s, h
}

// BenchmarkHandleWrite measures one 64 KiB kernel write over an open handle:
// the delta lands in the stored blob, so the bytes stored grow with the
// payload rather than with the file, and the handle holds no copy.
// See TheoryOfWriteSplice.
func BenchmarkHandleWrite(b *testing.B) {
	s, _ := newWriteBenchServer(b)
	payload := bytes.Repeat([]byte("x"), 64<<10)
	req := append(encodeStruct(&fuseWriteIn{Fh: 1, Offset: 0, Size: uint32(len(payload))}), payload...)
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.handleWrite(req); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWriteWholeBuffer measures the pre-campaign write path on one
// task: a 4 MiB file written sequentially in the conservative window
// (fuseMaxWrite, the most a kernel without MAX_PAGES negotiated), each
// window storing the accumulated content whole — the handle-buffer
// regime the splice path replaced. The comparison pair is
// BenchmarkWriteSpliceDelta. See TheoryOfWriteSplice and
// TheoryOfInitNegotiation.
func BenchmarkWriteWholeBuffer(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "sandbox.db"), b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	if err := store.WriteFile("/w.bin", nil, 0o644); err != nil {
		b.Fatal(err)
	}
	content := bytes.Repeat([]byte("x"), 4<<20)
	b.SetBytes(int64(len(content)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := store.Remove("/w.bin"); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		for off := 0; off < len(content); off += fuseMaxWrite {
			end := min(off+fuseMaxWrite, len(content))
			if err := store.WriteFile("/w.bin", content[:end], 0o644); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkWriteSpliceDelta measures the negotiated write path on the
// same task: a 4 MiB file written sequentially in 1 MiB windows
// (fuseMaxWriteMax, what MAX_PAGES buys), each window splicing its own
// payload into the store — no handle buffer, no whole-content copy
// through Go. The comparison pair is BenchmarkWriteWholeBuffer. See
// TheoryOfWriteSplice and TheoryOfInitNegotiation.
func BenchmarkWriteSpliceDelta(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "sandbox.db"), b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	if err := store.WriteFile("/w.bin", nil, 0o644); err != nil {
		b.Fatal(err)
	}
	content := bytes.Repeat([]byte("x"), 4<<20)
	b.SetBytes(int64(len(content)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := store.Remove("/w.bin"); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		for off := 0; off < len(content); off += fuseMaxWriteMax {
			end := min(off+fuseMaxWriteMax, len(content))
			if err := store.SpliceFile("/w.bin", int64(off), content[off:end], 0o644); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkHandleReaddirPlus measures one READDIRPLUS reply over a hundred
// entries: one request replaces a listing plus one LOOKUP per entry, which is
// the toolchain's dominant directory pattern. See TheoryOfInitNegotiation.
func BenchmarkHandleReaddirPlus(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "sandbox.db"), b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	s := newFuseServer(store, -1)
	entries := make([]DirEntry, 0, 102)
	for i := 0; i < 100; i++ {
		entries = append(entries, DirEntry{
			Name: "entry-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Attr: Attr{Kind: KindRegular, Mode: 0o644, Size: 16},
		})
	}
	s.handles[1] = &fuseHandle{path: "/d", isDir: true, entries: entries}
	req := encodeStruct(&fuseReadIn{Fh: 1, Size: 1 << 16})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.handleReaddirPlus(req); err != nil {
			b.Fatal(err)
		}
	}
}
