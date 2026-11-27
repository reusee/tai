package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsWritablePathResolvesSymlinkedParentOfMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Symlink(string(filepath.Separator), "root-link"); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	path := filepath.Join("root-link", "tai-path-that-does-not-exist")
	writable, err := IsWritablePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if writable {
		t.Fatalf("IsWritablePath(%q) = true, want false", path)
	}
}

// TestIsWritablePathAdmitsTaiDir checks the state directory rule: a path
// inside the tai dir is writable even when neither the working directory nor
// the static set covers it, which is the case for a session started in a
// module subdirectory. The probe lives under /usr, which exists but lies
// outside every writable directory, so the assertion isolates the tai dir
// entry; a host whose static set already covers the probe skips the test.
// See TheoryOfWritableDirs.
func TestIsWritablePathAdmitsTaiDir(t *testing.T) {
	const taiDir = "/usr"
	if info, err := os.Stat(taiDir); err != nil || !info.IsDir() {
		t.Skipf("%s is unavailable: %v", taiDir, err)
	}
	t.Chdir(t.TempDir())
	probe := filepath.Join(taiDir, "share", "tai-writable-probe")

	t.Setenv(taiDirEnv, "")
	before, err := IsWritablePath(probe)
	if err != nil {
		t.Fatal(err)
	}
	if before {
		t.Skipf("%s is already writable without the tai dir entry", probe)
	}

	t.Setenv(taiDirEnv, taiDir)
	after, err := IsWritablePath(probe)
	if err != nil {
		t.Fatal(err)
	}
	if !after {
		t.Fatal("a path inside the tai dir must be writable")
	}
}

// TestWritableDirsIncludesTaiDir checks that the directory list the focus
// check consumes names the tai dir the environment carries.
// See TheoryOfWritableDirs.
func TestWritableDirsIncludesTaiDir(t *testing.T) {
	taiDir := t.TempDir()
	t.Setenv(taiDirEnv, taiDir)

	dirs := WritableDirs()

	found := false
	for _, dir := range dirs {
		if dir == taiDir {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("tai dir %s missing from %v", taiDir, dirs)
	}
}

// TestSetTaiDirIgnoresEmpty checks that an unresolved tai dir leaves the
// environment alone instead of recording an empty value that every reader
// would have to filter.
// See TheoryOfWritableDirs.
func TestSetTaiDirIgnoresEmpty(t *testing.T) {
	t.Setenv(taiDirEnv, "")
	SetTaiDir("")
	if got := os.Getenv(taiDirEnv); got != "" {
		t.Fatalf("SetTaiDir(\"\") recorded %q", got)
	}
	dir := t.TempDir()
	SetTaiDir(dir)
	if got := os.Getenv(taiDirEnv); got != dir {
		t.Fatalf("SetTaiDir recorded %q, want %q", got, dir)
	}
}
