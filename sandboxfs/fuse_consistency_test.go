//go:build linux

package sandboxfs

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

// TestReaddirPlusDirentInoMatchesEntryNodeid protects TheoryOfFuseMount: one
// READDIRPLUS record names one inode. The entry_out's node id and the
// dirent's inode number come from the same lookup, so a tool that compares a
// listing's d_ino with a stat's st_ino sees one number.
func TestReaddirPlusDirentInoMatchesEntryNodeid(t *testing.T) {
	s := newInitTestServer(t)
	s.handles[1] = &fuseHandle{
		path:  "/d",
		isDir: true,
		entries: []DirEntry{
			{Name: "a.txt", Attr: Attr{Kind: KindRegular, Mode: 0o644, Size: 1}},
		},
	}
	out, err := s.handleReaddirPlus(encodeStruct(&fuseReadIn{Fh: 1, Size: 4096}))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < fuseEntryOutSize+fuseDirentSize {
		t.Fatalf("reply holds %d bytes, want one record", len(out))
	}
	nodeid := binary.LittleEndian.Uint64(out[0:])
	direntIno := binary.LittleEndian.Uint64(out[fuseEntryOutSize:])
	if nodeid == 0 {
		t.Fatal("the entry_out carries no node id")
	}
	if direntIno != nodeid {
		t.Errorf("dirent inode %d, entry node id %d; one record must name one inode", direntIno, nodeid)
	}
}

// TestReaddirDirentInoIsTheNodeId protects TheoryOfFuseMount: a plain
// listing names the same inode a LOOKUP of the same name would return, so
// the two views of one file agree.
func TestReaddirDirentInoIsTheNodeId(t *testing.T) {
	s := newInitTestServer(t)
	s.handles[1] = &fuseHandle{
		path:  "/d",
		isDir: true,
		entries: []DirEntry{
			{Name: "a.txt", Attr: Attr{Kind: KindRegular, Mode: 0o644, Size: 1}},
		},
	}
	out, err := s.handleReaddir(encodeStruct(&fuseReadIn{Fh: 1, Size: 4096}))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < fuseDirentSize {
		t.Fatalf("reply holds %d bytes, want one record", len(out))
	}
	ino := binary.LittleEndian.Uint64(out[0:])
	want, err := s.nodeOf("/d/a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if ino != want {
		t.Errorf("dirent inode %d, node id %d; a listing and a lookup must agree", ino, want)
	}
}

// TestFuseServerCreateInvalidatesCachedBaseReader protects TheoryOfReadPath:
// a truncation drops every reader cached for the path, and a truncating
// CREATE is a truncation. A handle that opened the file before the create
// must serve the store's new content, not the descriptor that fixes the old
// view.
func TestFuseServerCreateInvalidatesCachedBaseReader(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "a.txt", "original")
	s := newFuseServer(store, -1)
	h := &fuseHandle{path: "/a.txt", mode: 0o644}
	s.handles[7] = h
	s.nextFh = 8
	got, err := s.readFrom(h, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("the handle read %q before the create, want original", got)
	}
	if h.base == nil {
		t.Fatal("the read left no cached reader to invalidate")
	}
	root, err := s.nodeOf("/", true)
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, binary.Size(fuseCreateIn{}))
	binary.LittleEndian.PutUint32(body[0:], uint32(unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC))
	binary.LittleEndian.PutUint32(body[4:], 0o644)
	body = append(body, "a.txt\x00"...)
	if _, err := s.handleCreate(root, body); err != nil {
		t.Fatalf("create with O_TRUNC: %v", err)
	}
	got, err = s.readFrom(h, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("after the truncating create the handle read %q, want an empty file", got)
	}
}
