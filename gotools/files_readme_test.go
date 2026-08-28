package gotools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestModuleRootMarkdownListedWhenNoRootGoFiles(t *testing.T) {
	// Module-root markdown files are no longer package files: a module
	// root may carry no Go package, so its markdown is enumerated by
	// GetModuleRootFiles and emitted as a separate listing part instead
	// of being emitted at full content. See TheoryOfNonGoFiles in
	// module_root.go.
	root := t.TempDir()

	// Create go.mod at the module root
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create README.md at the module root (no .go files at root)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Test Project\n\nThis is a test."), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a Go package in a subdirectory so the module root has no
	// direct .go files and does not appear in rootPkgDirs.
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
		getModuleRootFiles GetModuleRootFiles,
	) {
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if filepath.Base(f.Path) == "README.md" {
				t.Fatalf("README.md must not be a package file, got %s", f.Path)
			}
		}

		listings, err := getModuleRootFiles()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, listing := range listings {
			if filepath.Clean(listing.Dir) != filepath.Clean(root) {
				continue
			}
			found = true
			readmePath := filepath.Join(root, "README.md")
			foundReadme := false
			for _, path := range listing.Files {
				if filepath.Clean(path) == filepath.Clean(readmePath) {
					foundReadme = true
				}
			}
			if !foundReadme {
				t.Fatalf("module-root listing of %s must contain README.md, got %v", root, listing.Files)
			}
		}
		if !found {
			t.Fatalf("module root %s must be listed even when it has no .go files, got %+v", root, listings)
		}
	})
}

func TestModuleRootMarkdownSkeletonsIncluded(t *testing.T) {
	// Module-root markdown listings carry a parsed heading skeleton when
	// one is extractable, so the model sees each document's structure
	// without its full content. See TheoryOfNonGoFiles in module_root.go
	// and anytexts.TheoryOfContextSkeleton.
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Guide\n\nbody\n\n## Setup\n\nsteps\n"), 0644); err != nil {
		t.Fatal(err)
	}

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
		getModuleRootFiles GetModuleRootFiles,
	) {
		listings, err := getModuleRootFiles()
		if err != nil {
			t.Fatal(err)
		}
		readmePath := filepath.Join(root, "README.md")
		for _, listing := range listings {
			if filepath.Clean(listing.Dir) != filepath.Clean(root) {
				continue
			}
			skeleton, ok := listing.Skeletons[readmePath]
			if !ok {
				t.Fatalf("README.md must carry a skeleton, got skeletons %v", listing.Skeletons)
			}
			if !strings.Contains(skeleton, "Guide") || !strings.Contains(skeleton, "Setup") {
				t.Errorf("skeleton must contain the headings, got:\n%s", skeleton)
			}
			// Heading lines are indented under the file name, so the
			// skeleton renders as an outline of the listing.
			if strings.Contains(skeleton, "# Guide") {
				t.Errorf("skeleton must strip heading markers, got:\n%s", skeleton)
			}
		}
	})
}

func TestModuleRootStructuralTextListed(t *testing.T) {
	// Module-root files in any gotreesitter-registered format are listed,
	// not only markdown: detection is registry-driven, so formats the
	// grammar library adds later are listed automatically. See
	// TheoryOfNonGoFiles in module_root.go and
	// anytexts.TheoryOfContextSkeleton.
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte("def handler(request):\n    return request\n"), 0644); err != nil {
		t.Fatal(err)
	}

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
		getModuleRootFiles GetModuleRootFiles,
	) {
		listings, err := getModuleRootFiles()
		if err != nil {
			t.Fatal(err)
		}
		pyPath := filepath.Join(root, "app.py")
		found := false
		for _, listing := range listings {
			if filepath.Clean(listing.Dir) != filepath.Clean(root) {
				continue
			}
			for _, path := range listing.Files {
				if filepath.Clean(path) == filepath.Clean(pyPath) {
					found = true
				}
			}
			skeleton, ok := listing.Skeletons[pyPath]
			if !ok {
				t.Fatalf("app.py must carry a skeleton, got skeletons %v", listing.Skeletons)
			}
			if !strings.Contains(skeleton, "handler") {
				t.Errorf("skeleton must contain the python function name, got:\n%s", skeleton)
			}
		}
		if !found {
			t.Fatalf("module root listing must contain app.py, got %+v", listings)
		}
	})
}
