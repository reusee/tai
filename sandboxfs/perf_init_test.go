package sandboxfs

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
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

// TestInitNegotiatesFastPath checks the INIT reply: the kernel's offered
// capabilities are echoed, MAX_PAGES raises the request window to
// fuseMaxWriteMax, a kernel that offers no MAX_PAGES keeps the conservative
// window, and the announced queue depth and readahead window carry the
// values the read path depends on. A shallow queue throttles the kernel's
// background reads, so the readahead window would promise more than the
// server ever receives; nothing else fails when that regresses.
// See TheoryOfInitNegotiation.
func TestInitNegotiatesFastPath(t *testing.T) {
	const offeredReadahead = 1 << 17
	for _, tc := range []struct {
		name     string
		flags    uint32
		maxWrite uint32
		maxPages uint16
	}{
		{name: "max pages", flags: fuseInitWanted, maxWrite: fuseMaxWriteMax, maxPages: fuseMaxPages},
		{name: "conservative", flags: fuseInitBigWrites, maxWrite: fuseMaxWrite, maxPages: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newInitTestServer(t)
			reply, err := s.handleInit(encodeStruct(&fuseInitIn{
				Major:        7,
				Minor:        fuseProtocolMinor,
				MaxReadahead: offeredReadahead,
				Flags:        tc.flags,
			}))
			if err != nil {
				t.Fatal(err)
			}
			var out fuseInitOut
			decodeStruct(&out, reply)
			if out.MaxWrite != tc.maxWrite {
				t.Fatalf("max_write = %d, want %d", out.MaxWrite, tc.maxWrite)
			}
			if out.MaxPages != tc.maxPages {
				t.Fatalf("max_pages = %d, want %d", out.MaxPages, tc.maxPages)
			}
			if out.Flags != tc.flags {
				t.Fatalf("flags = %#x, want the offered %#x", out.Flags, tc.flags)
			}
			if out.Minor != fuseProtocolMinor {
				t.Fatalf("minor = %d, want %d", out.Minor, fuseProtocolMinor)
			}
			if out.MaxReadahead != offeredReadahead {
				t.Fatalf("max_readahead = %d, want the offered %d", out.MaxReadahead, offeredReadahead)
			}
			if out.MaxBackground != fuseMaxBackground {
				t.Fatalf("max_background = %d, want %d", out.MaxBackground, fuseMaxBackground)
			}
			if out.CongestionThreshold != fuseCongestionThreshold {
				t.Fatalf("congestion_threshold = %d, want %d", out.CongestionThreshold, fuseCongestionThreshold)
			}
		})
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

// newLargeListingServer builds a server whose base holds files entries named
// f000.. so a listing test reads a directory larger than one window.
// See TheoryOfDirectoryStream.
func newLargeListingServer(t *testing.T, files int) (*fuseServer, uint64) {
	t.Helper()
	base := t.TempDir()
	for i := 0; i < files; i++ {
		if err := os.WriteFile(filepath.Join(base, fmt.Sprintf("f%03d", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s := newFuseServer(store, -1)
	root, err := s.nodeOf("/", true)
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

// openListing opens one directory handle and returns it with its number.
func openListing(t *testing.T, s *fuseServer, nodeid uint64) (*fuseHandle, uint64) {
	t.Helper()
	reply, err := s.handleOpendir(nodeid)
	if err != nil {
		t.Fatal(err)
	}
	var out fuseOpenOut
	decodeStruct(&out, reply)
	h := s.handles[out.Fh]
	if h == nil {
		t.Fatal("opendir registered no handle")
	}
	return h, out.Fh
}

// TestOpendirDoesNotEnumerateTheDirectory protects TheoryOfDirectoryStream:
// opening a listing reads the directory's own attributes, the upper layer's
// rows, and one handle on the base directory, never its entries. A request that
// enumerated the directory would make its duration grow with the directory's
// entry count and hold the server's one goroutine for the whole walk, and every
// other process's file I/O would wait behind it.
func TestOpendirDoesNotEnumerateTheDirectory(t *testing.T) {
	s, root := newLargeListingServer(t, 300)
	h, _ := openListing(t, s, root)
	if h.dir == nil {
		t.Fatal("the directory handle carries no cursor")
	}
	if len(h.entries) != 2 {
		t.Fatalf("opendir materialised %d entries, want only \".\" and \"..\"", len(h.entries))
	}
	for _, e := range h.entries {
		if e.Name != "." && e.Name != ".." {
			t.Fatalf("opendir materialised the base entry %q", e.Name)
		}
	}
}

// TestReaddirWindowsPullOneWindow protects TheoryOfDirectoryStream: every
// READDIR reply costs at most the window it carries, and continuing the
// listing until the end reports every name exactly once. The announced window
// is the negotiated megabyte, which could hold far more entries than one
// window; the reply stops there and the kernel asks again at the cookie. A
// reply that pulled the whole directory would make the request's duration grow
// with the directory's size.
func TestReaddirWindowsPullOneWindow(t *testing.T) {
	const files = 600
	s, root := newLargeListingServer(t, files)
	h, fh := openListing(t, s, root)
	seen := make(map[string]bool)
	off := 0
	replies := 0
	for {
		before := len(h.entries)
		reply, err := s.handleReaddir(encodeStruct(&fuseReadIn{
			Fh: fh, Offset: uint64(off), Size: fuseMaxWriteMax,
		}))
		if err != nil {
			t.Fatal(err)
		}
		if len(reply) == 0 {
			break
		}
		replies++
		if got := len(h.entries) - before; got > dirBatchEntries {
			t.Fatalf("one reply pulled %d entries, want at most %d", got, dirBatchEntries)
		}
		for rest := reply; len(rest) >= fuseDirentSize; {
			namelen := int(binary.LittleEndian.Uint32(rest[16:]))
			if fuseDirentSize+namelen > len(rest) {
				t.Fatalf("truncated record: %d bytes left, name length %d", len(rest), namelen)
			}
			name := string(rest[fuseDirentSize : fuseDirentSize+namelen])
			if seen[name] {
				t.Fatalf("name %q reported twice", name)
			}
			seen[name] = true
			off++
			rest = rest[(fuseDirentSize+namelen+7)&^7:]
		}
	}
	if len(seen) != files+2 {
		t.Fatalf("the listing reported %d names, want %d", len(seen), files+2)
	}
	if replies < (files+2)/dirBatchEntries {
		t.Fatalf("the listing fit in %d replies, want the directory to span several windows", replies)
	}
	for _, name := range []string{".", ".."} {
		if !seen[name] {
			t.Fatalf("the listing holds no %q entry", name)
		}
	}
	for i := 0; i < files; i += files / 10 {
		if !seen[fmt.Sprintf("f%03d", i)] {
			t.Fatalf("the listing holds no f%03d entry", i)
		}
	}
	if got := cap(h.listBuf); got > fuseMaxWriteMax {
		t.Fatalf("scratch holds %d bytes, want the entries' size rather than the announced window", got)
	}
}

// TestReaddirContinuesAfterAMidWalkMutation protects TheoryOfDirectoryStream:
// a create through the mount marks an open listing stale, and the walk the
// create interrupted must still report the rest of its entries. Resetting the
// stream would answer the kernel's next cookie with an empty body, which the
// kernel reads as the end of the directory, and a walk of a large tree would
// lose every entry past the mutation.
func TestReaddirContinuesAfterAMidWalkMutation(t *testing.T) {
	const files = 600
	s, root := newLargeListingServer(t, files)
	_, fh := openListing(t, s, root)
	seen := make(map[string]bool)
	off := 0
	read := func() bool {
		t.Helper()
		reply, err := s.handleReaddir(encodeStruct(&fuseReadIn{
			Fh: fh, Offset: uint64(off), Size: fuseMaxWriteMax,
		}))
		if err != nil {
			t.Fatal(err)
		}
		if len(reply) == 0 {
			return false
		}
		for rest := reply; len(rest) >= fuseDirentSize; {
			namelen := int(binary.LittleEndian.Uint32(rest[16:]))
			if fuseDirentSize+namelen > len(rest) {
				t.Fatalf("truncated record: %d bytes left, name length %d", len(rest), namelen)
			}
			seen[string(rest[fuseDirentSize:fuseDirentSize+namelen])] = true
			off++
			rest = rest[(fuseDirentSize+namelen+7)&^7:]
		}
		return true
	}
	if !read() {
		t.Fatal("the first listing reply is empty")
	}
	createIn := fuseCreateIn{Flags: uint32(syscall.O_WRONLY | syscall.O_CREAT), Mode: 0o644}
	if _, err := s.handleCreate(root, append(encodeStruct(&createIn), "new.txt\x00"...)); err != nil {
		t.Fatal(err)
	}
	for read() {
	}
	if len(seen) != files+2 {
		t.Fatalf("the interrupted walk reported %d names, want %d", len(seen), files+2)
	}
}
