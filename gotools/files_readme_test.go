package gotools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestFilesIncludesRootMarkdownWhenNoRootGoFiles(t *testing.T) {
	root := t.TempDir()

	// Create go.mod at the module root
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create README.md at the module root (no .go files at root)
	readmeContent := "# Test Project\n\nThis is a test."
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(readmeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a Go package in a subdirectory so the module root has no
	// direct .go files and does not appear in rootPkgDirs naturally.
	subDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "pkg.go"), []byte("package pkg\n\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(root)
		},
	).Call(func(
		getFiles GetFiles,
	) {
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}

		foundReadme := false
		for _, f := range files {
			if filepath.Base(f.Path) == "README.md" {
				foundReadme = true
				if !f.PackageIsRoot {
					t.Error("README.md should be marked as PackageIsRoot")
				}
				break
			}
		}
		if !foundReadme {
			t.Fatal("README.md should be discovered even when the module root has no .go files")
		}
	})
}
