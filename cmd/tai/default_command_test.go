package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirHasGoModuleInCurrentDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !dirHasGoModule(dir) {
		t.Fatal("expected dirHasGoModule to return true when go.mod is in the given directory")
	}
}

func TestDirHasGoModuleInParentDir(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(rootDir, "sub", "dir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if !dirHasGoModule(subDir) {
		t.Fatal("expected dirHasGoModule to return true when go.mod is in a parent directory")
	}
}

func TestDirHasGoModuleWithoutGoMod(t *testing.T) {
	dir := t.TempDir()
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		t.Fatal("temp directory unexpectedly contains go.mod")
	}
	if dirHasGoModule(dir) {
		t.Skip("temp directory hierarchy contains a go.mod; cannot test false case in this environment")
	}
}
