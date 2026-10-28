//go:build linux

package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// newTestDirStore builds a store on the directory layer and returns it with
// the base directory and the record directory, so a test names each part.
func newTestDirStore(t *testing.T) (store *Store, base, dir string) {
	t.Helper()
	base = t.TempDir()
	dir = filepath.Join(t.TempDir(), "rec.d")
	store, err := OpenDir(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store, base, dir
}

// TestOpenDirRecordsToADirectory protects TheoryOfDirUpper: the directory layer
// records the session as ordinary files inside one directory — one metadata
// file per recorded path, one content file per content-bearing path, and the
// audit log — instead of a database. The store API runs end to end against it.
func TestOpenDirRecordsToADirectory(t *testing.T) {
	store, base, dir := newTestDirStore(t)
	if _, ok := store.upper.(*dirUpper); !ok {
		t.Fatalf("upper layer is %T, want *dirUpper", store.upper)
	}
	if store.dbPath != dir {
		t.Fatalf("record path %q, want %q", store.dbPath, dir)
	}

	if err := store.WriteFile("/a.txt", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadFile("/a.txt")
	if err != nil || string(got) != "hello" {
		t.Fatalf("content %q (error %v), want %q", got, err, "hello")
	}
	names := recordFiles(t, dir)
	for _, want := range []string{metaName("/a.txt"), dataName("/a.txt")} {
		if !slices.Contains(names, want) {
			t.Errorf("record lacks %s: %v", want, names)
		}
	}

	changes, err := store.Changes()
	if err != nil || len(changes) != 1 || changes[0].Kind != ChangeAdded {
		t.Fatalf("changes %+v (error %v), want one added path", changes, err)
	}
	applied, err := store.Apply(nil)
	if err != nil || len(applied) != 1 {
		t.Fatalf("applied %+v (error %v), want one change", applied, err)
	}
	if hostContent(t, base, "a.txt") != "hello" {
		t.Fatal("apply did not reach the host")
	}
	if changes, err := store.Changes(); err != nil || len(changes) != 0 {
		t.Fatalf("changes after apply %+v (error %v), want none", changes, err)
	}
	if ops, err := store.Ops(); err != nil || len(ops) == 0 {
		t.Fatalf("audit %v (error %v), want records of the session", ops, err)
	}
}

// TestOpenDirMetadataOnlyLeavesNoContentFile protects TheoryOfBaseCapture
// against the directory layer: a chmod of a base file records metadata alone,
// so the record holds one metadata file and no content file, and a partial
// write materialises the base's bytes into the content file it names.
func TestOpenDirMetadataOnlyLeavesNoContentFile(t *testing.T) {
	store, base, dir := newTestDirStore(t)
	writeHost(t, base, "f", "hello")
	if err := store.SetMode("/f", 0o600); err != nil {
		t.Fatal(err)
	}
	if content, err := store.ReadFile("/f"); err != nil || string(content) != "hello" {
		t.Fatalf("read after chmod: %q (error %v), want %q", content, err, "hello")
	}
	if slices.Contains(recordFiles(t, dir), dataName("/f")) {
		t.Error("a metadata-only change wrote a content file")
	}

	if err := store.SpliceFile("/f", 0, []byte("HE"), 0o600); err != nil {
		t.Fatalf("splice into the metadata-only row: %v", err)
	}
	if content, err := store.ReadFile("/f"); err != nil || string(content) != "HEllo" {
		t.Fatalf("read after splice: %q (error %v), want %q", content, err, "HEllo")
	}
	if !slices.Contains(recordFiles(t, dir), dataName("/f")) {
		t.Error("the splice did not write a content file")
	}
}

// TestOpenAnyRoutesDirectoryRecord protects TheoryOfDirRecord: the review
// command routes a record to its layer by shape, not by a flag. A directory
// path opens the directory layer; a file path opens the SQL layer.
func TestOpenAnyRoutesDirectoryRecord(t *testing.T) {
	base := t.TempDir()

	dirStore, err := OpenDir(filepath.Join(t.TempDir(), "rec.d"), base)
	if err != nil {
		t.Fatal(err)
	}
	dirPath := dirStore.dbPath
	if err := dirStore.Close(); err != nil {
		t.Fatal(err)
	}
	routed, err := OpenAny(dirPath, base)
	if err != nil {
		t.Fatal(err)
	}
	defer routed.Close()
	if _, ok := routed.upper.(*dirUpper); !ok {
		t.Fatalf("a directory record routed to %T, want *dirUpper", routed.upper)
	}

	dbPath := filepath.Join(t.TempDir(), "rec.db")
	dbStore, err := Open(dbPath, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := dbStore.Close(); err != nil {
		t.Fatal(err)
	}
	routedDB, err := OpenAny(dbPath, base)
	if err != nil {
		t.Fatal(err)
	}
	defer routedDB.Close()
	if _, ok := routedDB.upper.(*sqlUpper); !ok {
		t.Fatalf("a database record routed to %T, want *sqlUpper", routedDB.upper)
	}
}

// TestOpenDirProtectsTheRecordDirectory protects TheoryOfSandboxFilesystem and
// TheoryOfDirRecord: the record directory and every path beneath it are the
// review surface, so the store refuses a mutation of any of them. The record
// sits inside the base, so its stored path and its sandbox path agree — the
// shape a mounted store carries when the mount rewrites the record's path to
// its private view.
func TestOpenDirProtectsTheRecordDirectory(t *testing.T) {
	base := t.TempDir()
	store, err := OpenDir(filepath.Join(base, "rec.d"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, p := range []string{"/rec.d", "/rec.d/" + metaName("/x"), "/rec.d/audit.log"} {
		if err := store.WriteFile(p, []byte("x"), 0o644); !errors.Is(err, os.ErrPermission) {
			t.Errorf("write to %s: got %v, want a permission error", p, err)
		}
	}
	if err := store.WriteFile("/ok.txt", []byte("x"), 0o644); err != nil {
		t.Errorf("unrelated write rejected: %v", err)
	}
}

// TestOpenMountStoreSelectsDirRecord protects the mount's layer selection:
// DirRecord selects the directory layer at the record path the mount returns,
// and validateMountOptions rejects the marker without a record path and
// rejects the dialect and the directory marker together.
func TestOpenMountStoreSelectsDirRecord(t *testing.T) {
	base := t.TempDir()
	record := filepath.Join(t.TempDir(), "rec.d")
	store, absDB, err := openMountStore(MountOptions{DBPath: record, DirRecord: true}, base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, ok := store.upper.(*dirUpper); !ok {
		t.Fatalf("DirRecord selected %T, want *dirUpper", store.upper)
	}
	if absDB == "" || store.dbPath != absDB {
		t.Fatalf("record path %q, abs %q; want both set to the same directory", store.dbPath, absDB)
	}

	err = validateMountOptions(MountOptions{Mountpoint: "/tmp/mnt", DirRecord: true})
	if err == nil || !strings.Contains(err.Error(), "empty database path") {
		t.Fatalf("DirRecord without a record: err = %v", err)
	}
	err = validateMountOptions(MountOptions{Mountpoint: "/tmp/mnt", DuckDB: true, DirRecord: true, DBPath: record})
	if err == nil {
		t.Fatal("validateMountOptions accepted DuckDB and DirRecord together")
	}
}

// recordFiles lists the directory record's files, so a test can check what the
// layer wrote without depending on ordering.
func recordFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// TestDefaultRecordPathPrefersTheDirectoryRecord protects TheoryOfDirRecord: the
// review command finds the record a session wrote without a flag, and a
// directory record wins over a database file because a session that wrote one
// selected that layer.
func TestDefaultRecordPathPrefersTheDirectoryRecord(t *testing.T) {
	dir := t.TempDir()
	if got, want := DefaultRecordPath(dir), DefaultDBPath(dir); got != want {
		t.Errorf("no record: got %q, want the database default %q", got, want)
	}
	if err := os.MkdirAll(DefaultDirPath(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := DefaultRecordPath(dir), DefaultDirPath(dir); got != want {
		t.Errorf("directory record: got %q, want %q", got, want)
	}
}

// TestOpenDirRejectsAnEmptyRecord protects the constructor's guard: a record
// directory names where the session's files live, and an empty name names no
// place, so the open fails instead of writing beside the process.
func TestOpenDirRejectsAnEmptyRecord(t *testing.T) {
	if _, err := OpenDir("", t.TempDir()); err == nil {
		t.Fatal("OpenDir accepted an empty record path")
	}
}

// TestOpenDirWithPolicyRefusesWritesOutsideIt protects TheoryOfWritePolicy on
// the directory layer: the same union logic asks the policy before a mutation,
// so a refused write leaves no record file behind.
func TestOpenDirWithPolicyRefusesWritesOutsideIt(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(t.TempDir(), "rec.d")
	store, err := OpenDirWithPolicy(dir, base, func(p string) error {
		if strings.HasPrefix(p, "/allowed") {
			return nil
		}
		return os.ErrPermission
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.WriteFile("/denied", []byte("x"), 0o644); !errors.Is(err, os.ErrPermission) {
		t.Errorf("write outside the policy: got %v, want a permission error", err)
	}
	if slices.Contains(recordFiles(t, dir), metaName("/denied")) {
		t.Error("the refused write left a record file")
	}
	if err := store.WriteFile("/allowed", []byte("x"), 0o644); err != nil {
		t.Errorf("write inside the policy: %v", err)
	}
}
