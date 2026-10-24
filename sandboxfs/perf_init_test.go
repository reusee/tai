package sandboxfs

import (
	"encoding/binary"
	"path/filepath"
	"slices"
	"testing"
)

// newInitTestServer builds a server over an empty base for the INIT and
// READDIRPLUS tests: both handlers answer from the server's own state and the
// store, so no kernel connection is needed.
func newInitTestServer(t *testing.T) *fuseServer {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return newFuseServer(store, -1)
}

// TestInitNegotiatesFastPath checks the INIT reply that carries the fast path:
// the kernel's offered capabilities are echoed, and MAX_PAGES raises the request
// window to fuseMaxWriteMax. See TheoryOfInitNegotiation.
func TestInitNegotiatesFastPath(t *testing.T) {
	s := newInitTestServer(t)
	reply, err := s.handleInit(encodeStruct(&fuseInitIn{
		Major:        7,
		Minor:        fuseProtocolMinor,
		MaxReadahead: 1 << 17,
		Flags:        fuseInitWanted,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var out fuseInitOut
	decodeStruct(&out, reply)
	if out.MaxWrite != fuseMaxWriteMax {
		t.Fatalf("max_write = %d, want %d", out.MaxWrite, fuseMaxWriteMax)
	}
	if out.MaxPages != fuseMaxPages {
		t.Fatalf("max_pages = %d, want %d", out.MaxPages, fuseMaxPages)
	}
	if out.Flags != fuseInitWanted {
		t.Fatalf("flags = %#x, want %#x", out.Flags, fuseInitWanted)
	}
	if out.Flags&fuseInitBigWrites == 0 {
		t.Fatal("BIG_WRITES not negotiated")
	}
	if out.Minor != fuseProtocolMinor {
		t.Fatalf("minor = %d, want %d", out.Minor, fuseProtocolMinor)
	}
}

// TestInitKeepsConservativeWindow checks the fallback: a kernel that offers no
// MAX_PAGES keeps the one-page write ceiling out of the negotiation, so the
// announced window stays fuseMaxWrite.
func TestInitKeepsConservativeWindow(t *testing.T) {
	s := newInitTestServer(t)
	reply, err := s.handleInit(encodeStruct(&fuseInitIn{
		Major: 7,
		Minor: fuseProtocolMinor,
		Flags: fuseInitBigWrites,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var out fuseInitOut
	decodeStruct(&out, reply)
	if out.MaxWrite != fuseMaxWrite {
		t.Fatalf("max_write = %d, want %d", out.MaxWrite, fuseMaxWrite)
	}
	if out.MaxPages != 0 {
		t.Fatalf("max_pages = %d, want 0", out.MaxPages)
	}
	if out.Flags&fuseInitMaxPages != 0 {
		t.Fatal("MAX_PAGES echoed although the kernel did not offer it")
	}
	if out.Flags&fuseInitBigWrites == 0 {
		t.Fatal("BIG_WRITES not echoed although the kernel offered it")
	}
}

// TestReaddirPlusCarriesEntryOut checks that READDIRPLUS answers every entry
// with its name and the entry_out node id the kernel would otherwise LOOKUP, so
// one listing replaces a listing plus one LOOKUP per entry.
func TestReaddirPlusCarriesEntryOut(t *testing.T) {
	s := newInitTestServer(t)
	s.handles[7] = &fuseHandle{
		path:  "/d",
		isDir: true,
		entries: []DirEntry{
			{Name: ".", Attr: Attr{Kind: KindDir, Mode: 0o755}},
			{Name: "a.txt", Attr: Attr{Kind: KindRegular, Mode: 0o644, Size: 5}},
		},
	}
	reply, err := s.handleReaddirPlus(encodeStruct(&fuseReadIn{Fh: 7, Size: 4096}))
	if err != nil {
		t.Fatal(err)
	}
	entryOutSize := binary.Size(fuseEntryOut{})
	var names []string
	rest := reply
	for len(rest) >= entryOutSize+fuseDirentSize {
		nameLen := int(binary.LittleEndian.Uint32(rest[entryOutSize+16:]))
		if entryOutSize+fuseDirentSize+nameLen > len(rest) {
			t.Fatalf("truncated record: %d bytes left, name length %d", len(rest), nameLen)
		}
		name := string(rest[entryOutSize+fuseDirentSize : entryOutSize+fuseDirentSize+nameLen])
		if binary.LittleEndian.Uint64(rest[0:]) == 0 {
			t.Fatalf("entry %q carries no node id", name)
		}
		names = append(names, name)
		rest = rest[(entryOutSize+fuseDirentSize+nameLen+7)&^7:]
	}
	if len(names) != 2 || names[0] != "." || names[1] != "a.txt" {
		t.Fatalf("entries = %v", names)
	}
}

// TestReaddirPlusSizesScratchFromEntries checks the reply scratch of one
// listing: it holds the entries that fit rather than the announced request
// window, and a repeated listing of the same handle reuses it — same bytes,
// no growth. The kernel announces the negotiated 1 MiB window even for a
// small directory, so sizing the scratch from the request reserves far more
// than the listing uses, and reusing it requires every record to be written
// whole. See TheoryOfInitNegotiation.
func TestReaddirPlusSizesScratchFromEntries(t *testing.T) {
	s := newInitTestServer(t)
	entries := make([]DirEntry, 0, 8)
	for i := 0; i < 4; i++ {
		entries = append(entries, DirEntry{
			Name: "f" + string(rune('a'+i)),
			Attr: Attr{Kind: KindRegular, Mode: 0o644, Size: 3},
		})
	}
	h := &fuseHandle{path: "/d", isDir: true, entries: entries}
	s.handles[3] = h
	req := encodeStruct(&fuseReadIn{Fh: 3, Size: fuseMaxWriteMax})
	first, err := s.handleReaddirPlus(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := cap(h.listBuf); got > 4<<10 {
		t.Fatalf("scratch holds %d bytes for a %d-byte reply; it must size from the entries, not the announced window", got, len(first))
	}
	again, err := s.handleReaddirPlus(req)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(first) {
		t.Fatalf("repeated listing changed the reply: %q, want %q", again, first)
	}
	if got := cap(h.listBuf); got > 4<<10 {
		t.Fatalf("scratch grew to %d bytes on a repeated listing", got)
	}
}

// TestReaddirSizesScratchFromEntries checks the plain READDIR reply: the
// scratch is sized from the entries that fit instead of the announced window,
// every entry decodes from the reply, and a repeated listing reuses the
// scratch without growing it. See TheoryOfFuseMount.
func TestReaddirSizesScratchFromEntries(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := newFuseServer(store, -1)
	names := []string{"alpha", "beta", "gamma"}
	entries := make([]DirEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, DirEntry{
			Name: name,
			Attr: Attr{Kind: KindRegular, Mode: 0o644, Size: 1},
		})
	}
	s.handles[1] = &fuseHandle{path: "/d", isDir: true, entries: entries}
	req := encodeStruct(&fuseReadIn{Fh: 1, Size: 1 << 16})
	out, err := s.handleReaddir(req)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for off := 0; off+fuseDirentSize <= len(out); {
		namelen := int(binary.LittleEndian.Uint32(out[off+16:]))
		got = append(got, string(out[off+fuseDirentSize:off+fuseDirentSize+namelen]))
		off += (fuseDirentSize + namelen + 7) &^ 7
	}
	if !slices.Equal(got, names) {
		t.Fatalf("entries = %v, want %v", got, names)
	}
	if cap(s.handles[1].listBuf) >= 1<<16 {
		t.Fatalf("scratch = %d bytes, want the entries' size rather than the announced window", cap(s.handles[1].listBuf))
	}
	before := cap(s.handles[1].listBuf)
	if _, err := s.handleReaddir(req); err != nil {
		t.Fatal(err)
	}
	if cap(s.handles[1].listBuf) != before {
		t.Fatalf("repeated listing grew the scratch: %d -> %d bytes", before, cap(s.handles[1].listBuf))
	}
}
