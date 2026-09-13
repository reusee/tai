package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootMkdirAll(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// Nested directories are created, like os.MkdirAll.
	if err := RootMkdirAll(root, filepath.Join("a", "b", "c"), 0755); err != nil {
		t.Fatal(err)
	}
	if info, err := root.Stat(filepath.Join("a", "b", "c")); err != nil || !info.IsDir() {
		t.Fatalf("expected directory a/b/c, got %v, %v", info, err)
	}

	// An existing directory succeeds (idempotent).
	if err := RootMkdirAll(root, filepath.Join("a", "b", "c"), 0755); err != nil {
		t.Fatal(err)
	}

	// A regular file occupying the path is an error, like os.MkdirAll:
	// returning nil would let a later write through the path fail with
	// an obscure ENOTDIR instead of the real cause.
	if err := root.WriteFile("occupied", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RootMkdirAll(root, "occupied", 0755); err == nil {
		t.Fatal("expected error for path occupied by a regular file, got nil")
	}

	// A directory beneath a regular file is an error too.
	if err := RootMkdirAll(root, filepath.Join("occupied", "sub"), 0755); err == nil {
		t.Fatal("expected error for path beneath a regular file, got nil")
	}
}
