package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
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
	s.invalidateHandles("/f.txt")
	if h.base != nil {
		t.Fatal("cached base reader survived invalidation")
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

func TestOwnDatabaseIsNeverASandboxTarget(t *testing.T) {
	s, _ := newTestStore(t)
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
	if _, err := s.db.Exec(
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
