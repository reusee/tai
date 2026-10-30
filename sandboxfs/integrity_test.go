package sandboxfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestWritePolicyCoversEveryMutation protects TheoryOfWritePolicy: the store
// asks the policy about every path it is about to mutate, before it changes
// anything, so a refused path is left untouched.
func TestWritePolicyCoversEveryMutation(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(filepath.Join(base, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	const refused = "/work/f"
	store, err := OpenWithPolicy(filepath.Join(dir, "upper.db"), base, func(p string) error {
		if p == refused {
			return fmt.Errorf("%w: %s", os.ErrPermission, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.WriteFile("/work/ok", []byte("ok"), 0o644); err != nil {
		t.Fatalf("the policy admits /work/ok, so the write must succeed: %v", err)
	}
	now := time.Now()
	ops := []struct {
		name string
		call func() error
	}{
		{"write", func() error { return store.WriteFile(refused, []byte("x"), 0o644) }},
		{"splice", func() error { return store.SpliceFile(refused, 0, []byte("x"), 0o644) }},
		{"truncate", func() error { return store.TruncateFile(refused, 0, 0o644) }},
		{"mkdir", func() error { return store.Mkdir(refused, 0o755) }},
		{"symlink", func() error { return store.Symlink("target", refused) }},
		{"mkfifo", func() error { return store.Mkfifo(refused, 0o644) }},
		{"chmod", func() error { return store.SetMode(refused, 0o600) }},
		{"utimes", func() error { return store.SetTimes(refused, now) }},
		{"remove", func() error { return store.Remove(refused) }},
	}
	for _, op := range ops {
		if err := op.call(); !errors.Is(err, os.ErrPermission) {
			t.Errorf("%s: got %v, want a permission error", op.name, err)
		}
	}
	if _, err := store.Stat(refused); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused path left a trace: %v", err)
	}
}

// TestSpliceFileZeroLengthKeepsTheLength protects TheoryOfCompletePOSIX: a
// zero-byte write transfers nothing and does not extend the file.
func TestSpliceFileZeroLengthKeepsTheLength(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.WriteFile("/f", []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/f", 100, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := store.Stat("/f")
	if err != nil {
		t.Fatal(err)
	}
	if a.Size != 3 {
		t.Errorf("size after a zero-byte write at offset 100: got %d, want 3", a.Size)
	}
	content, err := store.ReadFile("/f")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "abc" {
		t.Errorf("content after a zero-byte write: got %q, want %q", content, "abc")
	}
}

// TestOwnDatabaseIsRefusedThroughItsBasePath protects
// TheoryOfSandboxFilesystem: the store's own database is never a sandbox
// target, and a base that holds it gives the file a second path.
func TestOwnDatabaseIsRefusedThroughItsBasePath(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := DefaultDBPath(base)
	store, err := Open(dbPath, base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rel, err := filepath.Rel(base, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	name := "/" + filepath.ToSlash(rel)
	if err := store.WriteFile(name, []byte("not a database"), 0o644); !errors.Is(err, os.ErrPermission) {
		t.Errorf("writing the store database through %s: got %v, want a permission error", name, err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Path == name {
			t.Errorf("the store database appears as a change: %s", c.Path)
		}
	}
}

// TestSetModeAndSetTimesOnMissingPathReportNotExist protects
// TheoryOfCompletePOSIX: a mode or a time change names a path that must exist,
// so a path that exists in neither layer reports ENOENT and leaves no row.
func TestSetModeAndSetTimesOnMissingPathReportNotExist(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetMode("/missing", 0o600); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("chmod of a missing path: got %v, want a not-exist error", err)
	}
	if err := store.SetTimes("/missing", time.Now()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("utimes of a missing path: got %v, want a not-exist error", err)
	}
	if _, err := store.Stat("/missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused change left a row: %v", err)
	}
}

// TestDirectoryOverBaseFileIsAChange protects TheoryOfSandboxFilesystem: a
// directory the session puts where the base held a file is a net change, and
// applying it replaces the host file. A base directory records no content
// hash, so the recorded hash is what tells the two apart.
func TestDirectoryOverBaseFileIsAChange(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "d"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Remove("/d"); err != nil {
		t.Fatal(err)
	}
	if err := store.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "/d" || !changes[0].Dir {
		t.Fatalf("a directory over a base file: got %+v, want one directory change for /d", changes)
	}
	if _, err := store.Apply(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(base, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Errorf("applying the change left %v, want a directory", info.Mode())
	}
}

// TestApplyRejectsHostKindChange protects TheoryOfApplyConflict: a host path
// that turned into a directory after the session snapshot blocks the change,
// and nothing is applied.
func TestApplyRejectsHostKindChange(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.WriteFile("/f", []byte("session"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(base, "f")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "f"), 0o755); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Conflict == "" {
		t.Fatalf("a host kind change: got %+v, want one conflicting change", changes)
	}
	applied, err := store.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Errorf("a conflicting change was applied: %+v", applied)
	}
}

// TestApplyWritesAModeOnlyChange protects TheoryOfSandboxFilesystem and
// TheoryOfApplyConflict: a mode the session chose must reach the host even
// when the content and the kind stay the same. A content hash cannot see a
// permission bit, so the diff compares the recorded mode with the host's own;
// without that step a chmod is recorded in the store and silently dropped at
// apply, and a directory or a pipe is recreated instead of having its bits set.
func TestApplyWritesAModeOnlyChange(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(base, "f"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetMode("/f", 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMode("/d", 0o700); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("two mode-only changes: got %+v", changes)
	}
	if _, err := store.Apply(nil); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{"f": 0o600, "d": 0o700} {
		info, err := os.Stat(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s: host mode %o, want %o", name, info.Mode().Perm(), want)
		}
	}
	if content, err := os.ReadFile(filepath.Join(base, "f")); err != nil || string(content) != "x" {
		t.Errorf("the mode-only apply changed the content: %q %v", content, err)
	}
	if left, err := store.Changes(); err != nil || len(left) != 0 {
		t.Errorf("changes after apply: %+v %v", left, err)
	}
}

// TestWriteFileRefusesContentAboveTheBound protects TheoryOfCompletePOSIX: the
// store enforces the size bound on its own, so a store used without a mount
// refuses the same grow. A whole-file write is a grow like any other one, and
// content above the bound is refused with EFBIG before a row is allocated.
func TestWriteFileRefusesContentAboveTheBound(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tooBig := make([]byte, maxFileSize+1)
	if err := store.WriteFile("/big", tooBig, 0o644); !errors.Is(err, syscall.EFBIG) {
		t.Errorf("write above the bound: got %v, want EFBIG", err)
	}
	if _, err := store.Stat("/big"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused write left a row: %v", err)
	}
	if changes, err := store.Changes(); err != nil || len(changes) != 0 {
		t.Errorf("the refused write left a change: %+v %v", changes, err)
	}
}

// TestModeOnlyChangeKeepsTheTimes protects TheoryOfCompletePOSIX and
// TheoryOfApplyConflict: chmod records the bits and nothing else. The stored
// mtime stays the base file's, because a toolchain reads mtime to judge
// staleness, and the apply sets the bits on the node the host already holds
// instead of writing the same bytes into a fresh one.
func TestModeOnlyChangeKeepsTheTimes(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(base, "f")
	if err := os.WriteFile(host, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(host, 0o644); err != nil {
		t.Fatal(err)
	}
	want := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(host, want, want); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetMode("/f", 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := store.Stat("/f")
	if err != nil {
		t.Fatal(err)
	}
	if a.Mode != 0o600 {
		t.Errorf("stored mode %o, want 600", a.Mode)
	}
	if !a.ModTime.Equal(want) {
		t.Errorf("chmod changed the stored mtime: got %v, want %v", a.ModTime, want)
	}
	if _, err := store.Apply(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(host)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("applied mode %o, want 600", info.Mode().Perm())
	}
	if !info.ModTime().Equal(want) {
		t.Errorf("the apply rewrote the file instead of setting its bits: mtime %v, want %v",
			info.ModTime(), want)
	}
	if content, err := os.ReadFile(host); err != nil || string(content) != "x" {
		t.Errorf("the mode-only apply changed the content: %q %v", content, err)
	}
}

// TestApplyReplacesASymlinkWithAFileOfTheSameBytes protects
// TheoryOfApplyConflict: a mode-only change applies its bits to the node the
// host already holds, and that node must be the regular file the snapshot
// recorded. A base symbolic link whose target string equals the session's
// bytes compares equal by bytes and is still a change of kind, so the apply
// must replace the link rather than chmod a node that is not a file.
func TestApplyReplacesASymlinkWithAFileOfTheSameBytes(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(base, "f")); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Remove("/f"); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile("/f", []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != ChangeModified || changes[0].Symlink {
		t.Fatalf("a regular file over a base link: got %+v, want one modified change", changes)
	}
	if _, err := store.Apply(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(base, "f"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("the apply left %v, want a regular file", info.Mode())
	}
	if content, err := os.ReadFile(filepath.Join(base, "f")); err != nil || string(content) != "target" {
		t.Errorf("content after the apply: %q %v, want target", content, err)
	}
}

// TestModeOfNoBitsReachesTheHost protects TheoryOfCompletePOSIX and
// TheoryOfApplyConflict: the recorded mode is authoritative, 000 included. A
// chmod that takes every bit away is a change the session made, and a diff or
// an apply that reads zero as "no mode" hands the host bits the session never
// chose. A truncation records the mode it was handed, 000 included, and its
// materialisation substitutes no default of its own. The base file of that
// truncation stays readable, because the bytes a mode denies are bytes the
// store cannot copy.
func TestModeOfNoBitsReachesTheHost(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(base, "f")
	if err := os.WriteFile(host, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(host, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetMode("/f", 0); err != nil {
		t.Fatal(err)
	}
	if a, err := store.Stat("/f"); err != nil || a.Mode != 0 {
		t.Fatalf("stored mode %o (error %v), want 000", a.Mode, err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != ChangeModified || changes[0].Mode != 0 {
		t.Fatalf("a chmod to 000: got %+v, want one modified change of mode 000", changes)
	}
	if _, err := store.Apply(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0 {
		t.Errorf("applied mode %o, want 000", got)
	}
	// Restore a readable mode so the cleanup can inspect the tree, then check
	// the truncation path: a truncation records the mode it was handed, 000
	// included, and the materialisation that keeps a prefix must not put a
	// default in its place.
	if err := os.Chmod(host, 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(base, "g")
	if err := os.WriteFile(other, []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The truncation's capture reads the whole base file, and a fresh
	// write's effective mode can lack the owner's read bit, taken by the
	// umask or by the filesystem's own access rules. The bits are set
	// after the write, the same guard the host file above carries.
	if err := os.Chmod(other, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.TruncateFile("/g", 2, 0); err != nil {
		t.Fatal(err)
	}
	if a, err := store.Stat("/g"); err != nil || a.Mode != 0 || a.Size != 2 {
		t.Errorf("truncation to a mode of no bits: got mode %o size %d (error %v), want mode 000 size 2", a.Mode, a.Size, err)
	}
	// The same holds when the row already exists: the truncate shapes the
	// stored content and leaves the recorded bits alone.
	if err := store.WriteFile("/h", []byte("abcdef"), 0); err != nil {
		t.Fatal(err)
	}
	if err := store.TruncateFile("/h", 2, 0); err != nil {
		t.Fatal(err)
	}
	if a, err := store.Stat("/h"); err != nil || a.Mode != 0 || a.Size != 2 {
		t.Errorf("truncation of an upper row of no bits: got mode %o size %d (error %v), want mode 000 size 2", a.Mode, a.Size, err)
	}
}

// TestStatOfAMetadataOnlyRowWhoseHostPathVanishedReportsZeroSize protects
// TheoryOfReadPath and TheoryOfDirectoryStream: a metadata-only row takes its
// length from the base, and a base path the host no longer holds has no length.
// The store reports the zero the merging window reports for the same path,
// never the size sentinel, which a getattr would render as an enormous file
// while a listing of the same directory reported zero.
func TestStatOfAMetadataOnlyRowWhoseHostPathVanishedReportsZeroSize(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(base, "a.txt")
	if err := os.WriteFile(host, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "upper.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// A chmod records metadata only: the content still lives in the base, and
	// the row carries the size sentinel. See TheoryOfBaseCapture.
	if err := store.SetMode("/a.txt", 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(host); err != nil {
		t.Fatal(err)
	}
	a, err := store.Stat("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if a.Size != 0 {
		t.Errorf("stat of a vanished base path: size %d, want 0", a.Size)
	}
	entries, err := store.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Name != "a.txt" {
			continue
		}
		found = true
		if e.Attr.Size != a.Size {
			t.Errorf("listing size %d, stat size %d; the two must agree", e.Attr.Size, a.Size)
		}
	}
	if !found {
		t.Fatalf("the listing holds no a.txt entry: %+v", entries)
	}
}
