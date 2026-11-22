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
