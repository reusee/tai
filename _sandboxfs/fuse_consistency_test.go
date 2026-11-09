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

// TestFuseServerRenameKeepsOpenHandleAndNode protects the POSIX rename rule:
// renaming a name does not detach the open file description or allocate a new
// inode. The handle opened before the rename must read the same bytes, and the
// node id allocated before it must resolve to the new path.
func TestFuseServerRenameKeepsOpenHandleAndNode(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "a.txt", "content")
	c := newFuseClient(t, store)

	errno, resp := c.request(t, fuseOpLookup, fuseRootID, c.bodyWithNames(nil, "a.txt"))
	if errno != 0 {
		t.Fatalf("lookup errno %d", errno)
	}
	var entry fuseEntryOut
	decodeStruct(&entry, resp)
	aNode := entry.Nodeid
	errno, resp = c.request(t, fuseOpOpen, aNode, encodeStruct(&fuseOpenIn{}))
	if errno != 0 {
		t.Fatalf("open errno %d", errno)
	}
	var openOut fuseOpenOut
	decodeStruct(&openOut, resp)

	renameIn := fuseRenameIn{Newdir: fuseRootID}
	errno, _ = c.request(t, fuseOpRename, fuseRootID,
		c.bodyWithNames(encodeStruct(&renameIn), "a.txt", "b.txt"))
	if errno != 0 {
		t.Fatalf("rename errno %d", errno)
	}

	errno, resp = c.request(t, fuseOpRead, aNode, encodeStruct(&fuseReadIn{
		Fh: openOut.Fh, Size: 64,
	}))
	if errno != 0 || string(resp) != "content" {
		t.Errorf("read through the pre-rename handle: %q, errno %d; want content", resp, errno)
	}
	if errno, _ := c.request(t, fuseOpGetattr, aNode, nil); errno != 0 {
		t.Errorf("getattr through the pre-rename node: errno %d", errno)
	}
	errno, resp = c.request(t, fuseOpLookup, fuseRootID, c.bodyWithNames(nil, "b.txt"))
	if errno != 0 {
		t.Fatalf("lookup of the destination: errno %d", errno)
	}
	decodeStruct(&entry, resp)
	if entry.Nodeid != aNode {
		t.Errorf("destination node id %d, want the source's %d", entry.Nodeid, aNode)
	}
}

// TestFuseServerRenameDirectoryKeepsOpenListingAndNode covers the same rule
// through a subtree: an open directory stream resumes at the destination, and
// a child looked up before the move keeps its node id at the new path.
func TestFuseServerRenameDirectoryKeepsOpenListingAndNode(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "src/f.txt", "content")
	c := newFuseClient(t, store)

	errno, resp := c.request(t, fuseOpLookup, fuseRootID, c.bodyWithNames(nil, "src"))
	if errno != 0 {
		t.Fatalf("lookup of the source directory: errno %d", errno)
	}
	var srcEntry fuseEntryOut
	decodeStruct(&srcEntry, resp)
	errno, resp = c.request(t, fuseOpOpendir, srcEntry.Nodeid, encodeStruct(&fuseOpenIn{}))
	if errno != 0 {
		t.Fatalf("opendir errno %d", errno)
	}
	var openOut fuseOpenOut
	decodeStruct(&openOut, resp)
	errno, resp = c.request(t, fuseOpLookup, srcEntry.Nodeid, c.bodyWithNames(nil, "f.txt"))
	if errno != 0 {
		t.Fatalf("lookup of the child: errno %d", errno)
	}
	var childEntry fuseEntryOut
	decodeStruct(&childEntry, resp)
	childNode := childEntry.Nodeid

	renameIn := fuseRenameIn{Newdir: fuseRootID}
	errno, _ = c.request(t, fuseOpRename, fuseRootID,
		c.bodyWithNames(encodeStruct(&renameIn), "src", "dst"))
	if errno != 0 {
		t.Fatalf("rename errno %d", errno)
	}

	errno, resp = c.request(t, fuseOpReaddir, srcEntry.Nodeid, encodeStruct(&fuseReadIn{
		Fh: openOut.Fh, Size: 4096,
	}))
	if errno != 0 {
		t.Errorf("read the pre-rename directory handle: errno %d", errno)
	} else if names := parseDirentNames(t, resp); !contains(names, "f.txt") {
		t.Errorf("listing after the directory rename: %v, want f.txt", names)
	}
	if errno, _ := c.request(t, fuseOpGetattr, srcEntry.Nodeid, nil); errno != 0 {
		t.Errorf("getattr of the moved directory: errno %d", errno)
	}
	errno, resp = c.request(t, fuseOpLookup, srcEntry.Nodeid, c.bodyWithNames(nil, "f.txt"))
	if errno != 0 {
		t.Fatalf("lookup of the moved child: errno %d", errno)
	}
	decodeStruct(&childEntry, resp)
	if childEntry.Nodeid != childNode {
		t.Errorf("moved child node id %d, want %d", childEntry.Nodeid, childNode)
	}
}
