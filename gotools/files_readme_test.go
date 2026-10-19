package gotools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestModuleRootStructuralTextListed(t *testing.T) {
	// Module-root files in any gotreesitter-registered format are listed,
	// not only markdown: detection is registry-driven, so formats the
	// grammar library adds later are listed automatically. The module
	// root may carry no Go package, so its files are enumerated by
	// GetModuleFiles instead of being emitted as package files, and a
	// listed file carries a parsed skeleton when one is extractable.
	// See TheoryOfNonGoFiles in module_root.go and
	// anytexts.TheoryOfContextSkeleton.
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	readmePath := filepath.Join(root, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Guide\n\nbody\n\n## Setup\n\nsteps\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pyPath := filepath.Join(root, "app.py")
	if err := os.WriteFile(pyPath, []byte("def handler(request):\n    return request\n"), 0644); err != nil {
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

	scope = scope.Fork(
		func() LoadDir {
			return LoadDir(root)
		},
	)

	getFiles := scope.Get[GetFiles]()
	files, err := getFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if filepath.Base(f.Path) == "README.md" {
			t.Fatalf("README.md must not be a package file, got %s", f.Path)
		}
	}

	getModuleFiles := scope.Get[GetModuleFiles]()
	listings, err := getModuleFiles()
	if err != nil {
		t.Fatal(err)
	}
	var rootListing *ModuleFiles
	for i := range listings {
		if filepath.Clean(listings[i].Dir) == filepath.Clean(root) {
			rootListing = &listings[i]
		}
	}
	if rootListing == nil {
		t.Fatalf("module root %s must be listed even when it has no .go files, got %+v", root, listings)
	}

	listed := make(map[string]bool, len(rootListing.Files))
	for _, path := range rootListing.Files {
		listed[filepath.Clean(path)] = true
	}
	for _, want := range []string{readmePath, pyPath} {
		if !listed[filepath.Clean(want)] {
			t.Fatalf("module-root listing must contain %s, got %v", want, rootListing.Files)
		}
	}

	skeleton, ok := rootListing.Skeletons[readmePath]
	if !ok {
		t.Fatalf("README.md must carry a skeleton, got skeletons %v", rootListing.Skeletons)
	}
	if !strings.Contains(skeleton, "Guide") || !strings.Contains(skeleton, "Setup") {
		t.Errorf("markdown skeleton must contain the headings, got:\n%s", skeleton)
	}
	if strings.Contains(skeleton, "# Guide") {
		t.Errorf("markdown skeleton must strip heading markers, got:\n%s", skeleton)
	}

	skeleton, ok = rootListing.Skeletons[pyPath]
	if !ok {
		t.Fatalf("app.py must carry a skeleton, got skeletons %v", rootListing.Skeletons)
	}
	if !strings.Contains(skeleton, "handler") {
		t.Errorf("python skeleton must contain the function name, got:\n%s", skeleton)
	}
}

func TestModuleNonPackageDirsListed(t *testing.T) {
	// Every non-package directory under the module is enumerated, not
	// only the module root: a package directory produces no listing of
	// its own files but is still traversed, so non-package
	// subdirectories beneath it are listed too. Hidden and vendor
	// directories are skipped entirely. See TheoryOfNonGoFiles in
	// module_root.go.
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	guidePath := filepath.Join(docsDir, "guide.md")
	if err := os.WriteFile(guidePath, []byte("# Guide\n\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// A package directory with a nested non-package subdirectory: the
	// package's own files are package-anchored and never listed, but the
	// nested directory's structural text is listed.
	pkgDir := filepath.Join(root, "pkg")
	pkgDocsDir := filepath.Join(pkgDir, "docs")
	if err := os.MkdirAll(pkgDocsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	designPath := filepath.Join(pkgDocsDir, "design.md")
	if err := os.WriteFile(designPath, []byte("# Design\n\ndetail\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Hidden and vendor directories are skipped entirely.
	hiddenDir := filepath.Join(root, ".notes")
	if err := os.MkdirAll(hiddenDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "hidden.md"), []byte("# Hidden\n"), 0644); err != nil {
		t.Fatal(err)
	}
	vendorDir := filepath.Join(root, "vendor", "example.com", "dep")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "readme.md"), []byte("# Vendored\n"), 0644); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	scope = scope.Fork(
		func() LoadDir {
			return LoadDir(root)
		},
	)
	getModuleFiles := scope.Get[GetModuleFiles]()
	listings, err := getModuleFiles()
	if err != nil {
		t.Fatal(err)
	}
	filesOf := make(map[string][]string)
	for _, listing := range listings {
		filesOf[filepath.Clean(listing.Dir)] = listing.Files
	}

	files := filesOf[filepath.Clean(docsDir)]
	if len(files) != 1 || filepath.Clean(files[0]) != filepath.Clean(guidePath) {
		t.Fatalf("docs listing must contain guide.md, got %v", files)
	}

	files = filesOf[filepath.Clean(pkgDocsDir)]
	if len(files) != 1 || filepath.Clean(files[0]) != filepath.Clean(designPath) {
		t.Fatalf("pkg/docs listing must contain design.md, got %v", files)
	}

	if _, ok := filesOf[filepath.Clean(pkgDir)]; ok {
		t.Fatalf("package directory %s must not produce a listing", pkgDir)
	}

	for dir, files := range filesOf {
		for _, path := range files {
			if strings.Contains(path, ".notes") || strings.Contains(path, "vendor") {
				t.Errorf("hidden and vendor directories must be skipped, got %s in listing %s", path, dir)
			}
		}
	}
}
