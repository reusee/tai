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
