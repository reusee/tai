package sandboxfs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestApplyPreservesTheRecordedMode protects TheoryOfSandboxFilesystem: the
// upper layer records the mode beside the content, and applying a change
// writes both. A file the session created as 0600 must not reach the host as
// 0644, and a directory as 0755.
func TestApplyPreservesTheRecordedMode(t *testing.T) {
	s, base := newTestStore(t)
	if err := s.WriteFile("/a.txt", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Mkdir("/d", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Mkfifo("/p", 0o640); err != nil {
		t.Fatal(err)
	}
	applied, err := s.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 3 {
		t.Fatalf("applied %d changes, want 3", len(applied))
	}
	fileInfo, err := os.Lstat(filepath.Join(base, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("applied file mode %o, want 600", got)
	}
	dirInfo, err := os.Lstat(filepath.Join(base, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("applied directory mode %o, want 700", got)
	}
	fifoInfo, err := os.Lstat(filepath.Join(base, "p"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fifoInfo.Mode().Perm(); got != 0o640 {
		t.Errorf("applied pipe mode %o, want 640", got)
	}
}

// TestApplyKeepsTheModeOfAModifiedBaseFile pins the content-only path: a base
// file holds its own mode, the session changes the content and passes the base
// mode the way a caller that means to keep it does, and the store's stat and
// the applied host file both carry the base mode. See TheoryOfCompletePOSIX.
func TestApplyKeepsTheModeOfAModifiedBaseFile(t *testing.T) {
	s, base := newTestStore(t)
	host := filepath.Join(base, "a.txt")
	if err := os.WriteFile(host, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(host, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("/a.txt", []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := s.Stat("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if a.Mode != 0o600 {
		t.Errorf("stat after the write: got mode %o, want the base file's 600", a.Mode)
	}
	if _, err := s.Apply(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("applied mode %o, want the base file's 600", got)
	}
	content, err := os.ReadFile(host)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Errorf("applied content %q, want new", content)
	}
}

// TestApplySetsTheModeOfAModifiedFile protects TheoryOfSandboxFilesystem:
// the mode the row records accompanies the content. A truncating write into
// an existing host file keeps that file's old permission bits, so the apply
// must set the recorded ones explicitly instead of trusting the create.
func TestApplySetsTheModeOfAModifiedFile(t *testing.T) {
	s, base := newTestStore(t)
	host := filepath.Join(base, "a.txt")
	if err := os.WriteFile(host, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(host, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("/a.txt", []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("applied mode %o, want the session's 600", got)
	}
	content, err := os.ReadFile(host)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Errorf("applied content %q, want new", content)
	}
}

// TestApplyPreservesTheModePastTheUmask protects TheoryOfSandboxFilesystem:
// the mode the row records is the mode the host receives, even when the
// daemon's umask would mask the argument a create hands the kernel. The FUSE
// request already carries the caller's umask applied, so masking the mode a
// second time rewrites bits the session chose.
func TestApplyPreservesTheModePastTheUmask(t *testing.T) {
	old := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(old) })
	s, base := newTestStore(t)
	if err := s.WriteFile("/w", []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(base, "w"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Errorf("applied mode %o, want the recorded 666", got)
	}
}

// TestApplyWritesContentThroughAReadOnlyHostFile protects TheoryOfApplyWriteBits:
// the recorded mode is the state the host holds after the write, never a gate on
// the write. A tool rewrites an existing file by truncating it and writing the
// new bytes, and the truncation records the base file's own bits, so a host file
// the session may not write would answer the apply with a permission error while
// the store already holds the session's bytes. The node keeps its identity and
// ends with the recorded bits.
func TestApplyWritesContentThroughAReadOnlyHostFile(t *testing.T) {
	s, base := newTestStore(t)
	host := filepath.Join(base, "a.txt")
	if err := os.WriteFile(host, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The base file is read-only, the shape a release artifact or an earlier
	// loop's output leaves behind.
	if err := os.Chmod(host, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFile("/a.txt", 0, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := s.SpliceFile("/a.txt", 0, []byte("new"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(nil); err != nil {
		t.Fatalf("apply of a change to a read-only host file: %v", err)
	}
	content, err := os.ReadFile(host)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Errorf("applied content %q, want new", content)
	}
	info, err := os.Lstat(host)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o444 {
		t.Errorf("applied mode %o, want the recorded 444", got)
	}
}

// TestApplyWritesAChildThroughAReadOnlyDirectory protects
// TheoryOfApplyWriteBits: a directory's recorded mode is the state the host
// holds after the apply, never a gate on the apply of what the session put
// inside. A tree the session populated and then closed — the shape a toolchain
// leaves for a vendored or released directory — must reach the host, and the
// directory must end at the recorded bits.
func TestApplyWritesAChildThroughAReadOnlyDirectory(t *testing.T) {
	s, base := newTestStore(t)
	if err := s.Mkdir("/s", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Mkdir("/s/d", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("/s/d/f", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The session closes the directory after filling it. The store still
	// serves the child, because the upper layer does not read the parent's
	// bits; the apply must reach the same state.
	if err := s.SetMode("/s/d", 0o555); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(nil); err != nil {
		t.Fatalf("apply of a tree with a read-only directory: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(base, "s/d/f"))
	if err != nil {
		t.Fatalf("the child did not reach the host: %v", err)
	}
	if string(got) != "x" {
		t.Errorf("child content %q, want x", got)
	}
	info, err := os.Lstat(filepath.Join(base, "s/d"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o555 {
		t.Errorf("directory mode %o, want the recorded 555", info.Mode().Perm())
	}
}

// TestApplyDefersTheModeOfADirectory protects TheoryOfApplyWriteBits from the
// step the host's permission bits hide: a directory's recorded mode lands in
// the final phase, never in the creating phase, so the entries the session put
// inside a directory it then closed still reach the host. The test drives the
// two phases directly, because this process runs as root and the host refuses
// nothing it does.
func TestApplyDefersTheModeOfADirectory(t *testing.T) {
	s, base := newTestStore(t)
	if err := s.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("/d/f", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMode("/d", 0o500); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes()
	if err != nil {
		t.Fatal(err)
	}
	run := &applyRun{}
	for _, c := range changes {
		if c.Conflict != "" {
			t.Fatalf("unexpected conflict: %+v", c)
		}
		if err := s.applyOne(c, run); err != nil {
			t.Fatalf("apply %s %s: %v", c.Kind, c.Path, err)
		}
	}
	info, err := os.Lstat(filepath.Join(base, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm() & 0o700; got != 0o700 {
		t.Fatalf("the creating phase left /d at %o; the entries the session put inside it cannot land", info.Mode().Perm())
	}
	if len(run.pending) != 1 || run.pending[0].perm != 0o500 {
		t.Fatalf("the creating phase deferred %+v, want one mode of 500", run.pending)
	}
	if failures := run.finish(s); len(failures) != 0 {
		t.Fatalf("final phase: %+v", failures)
	}
	info, err = os.Lstat(filepath.Join(base, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o500 {
		t.Errorf("after the final phase /d is %o, want the recorded 500", got)
	}
	got, err := os.ReadFile(filepath.Join(base, "d/f"))
	if err != nil {
		t.Fatalf("the child did not reach the host: %v", err)
	}
	if string(got) != "x" {
		t.Errorf("child content %q, want x", got)
	}
}
