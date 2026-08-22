package gotools

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func TestFiles(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	dir := filepath.Join(testdataDir, "main")
	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		getFiles GetFiles,
	) {
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}

		// Find expected files by path; do not assume a particular order.
		var mainFile, aTxtFile, dep1File *File
		for _, f := range files {
			switch f.Path {
			case filepath.Join(dir, "main.go"):
				mainFile = f
			case filepath.Join(dir, "a.txt"):
				aTxtFile = f
			case filepath.Join(dir, "..", "dep1", "dep1.go"):
				dep1File = f
			}
		}
		if mainFile == nil {
			t.Fatal("main.go not found")
		}
		if aTxtFile == nil {
			t.Fatal("a.txt not found")
		}
		if dep1File == nil {
			t.Fatal("dep1.go not found")
		}

		// main.go checks
		if mainFile.TokenFile == nil {
			t.Error("main.go TokenFile is nil")
		}
		if mainFile.AstFile == nil {
			t.Error("main.go AstFile is nil")
		}
		if mainFile.Package == nil {
			t.Error("main.go Package is nil")
		}
		if !mainFile.PackageIsRoot {
			t.Error("main.go not marked as root package")
		}
		if mainFile.Module == nil {
			t.Error("main.go Module is nil")
		}
		if !mainFile.ModuleIsRoot {
			t.Error("main.go not marked as root module")
		}

		// a.txt checks
		if aTxtFile.Package == nil {
			t.Error("a.txt Package is nil")
		}
		if !aTxtFile.PackageIsRoot {
			t.Error("a.txt not marked as root package")
		}
		if aTxtFile.Module == nil {
			t.Error("a.txt Module is nil")
		}
		if !aTxtFile.ModuleIsRoot {
			t.Error("a.txt not marked as root module")
		}

		// dep1.go checks
		// PackageDistanceFromRoot is no longer computed by GetFiles; it is
		// computed by computeDistances in logical_package.go during
		// simplification. GetFiles sets it to 0 as a placeholder, overwritten
		// by simplify.go. Distance correctness is verified indirectly through
		// TestSimplify, which checks output ordering dependent on distance.
		if dep1File.TokenFile == nil {
			t.Error("dep1.go TokenFile is nil")
		}
		if dep1File.AstFile == nil {
			t.Error("dep1.go AstFile is nil")
		}
		if dep1File.Package == nil {
			t.Error("dep1.go Package is nil")
		}
		if dep1File.PackageIsRoot {
			t.Error("dep1.go incorrectly marked as root package")
		}
		if dep1File.Module == nil {
			t.Error("dep1.go Module is nil")
		}
		if !dep1File.ModuleIsRoot {
			t.Error("dep1.go not marked as root module")
		}

	})

}

func TestFilesGoFileContentCached(t *testing.T) {
	// Go file content must be cached at load time so the simplification
	// pipeline renders from memory instead of re-reading every file from
	// disk per visibility level. See TheoryOfFileLoadingPerformance.
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	dir := filepath.Join(testdataDir, "main")
	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		getFiles GetFiles,
	) {
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}
		var mainFile *File
		for _, f := range files {
			if f.Path == filepath.Join(dir, "main.go") {
				mainFile = f
				break
			}
		}
		if mainFile == nil {
			t.Fatal("main.go not found")
		}
		if len(mainFile.Content) == 0 {
			t.Fatal("main.go Content should be populated at load time")
		}
		disk, err := os.ReadFile(mainFile.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(mainFile.Content, disk) {
			t.Fatal("main.go Content should match the file on disk")
		}
	})
}

func TestCodeProviderMatchFlagFiltersFiles(t *testing.T) {
	// The -match regex include filter applies to the project files the
	// gotools pipeline assembles into the context — through the
	// CodeProvider's injected NameMatch, the same filter the anytexts
	// pipeline uses — so the flag works uniformly across the go and any
	// commands. See anytexts.TheoryOfMatchFiltering.
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	dir := filepath.Join(testdataDir, "main")
	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
		func() flags.Match {
			return flags.Match{`main\.go$`: true}
		},
	).Call(func(
		provider CodeProvider,
	) {
		parts, err := provider.Parts(1<<20, generators.DeepseekTokenCounterFn, nil)
		if err != nil {
			t.Fatal(err)
		}
		foundFocusDoc := false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				s := string(text)
				if strings.Contains(s, "begin of focus file "+filepath.Join(dir, "a.txt")) {
					t.Fatalf("expected a.txt to be excluded by -match, got: %s", s)
				}
				if strings.Contains(s, filepath.Join(dir, "..", "dep1", "dep1.go")) {
					t.Fatalf("expected dep1.go to be excluded by -match, got: %s", s)
				}
				if strings.Contains(s, "begin of focus package") {
					foundFocusDoc = true
				}
			}
		}
		if !foundFocusDoc {
			t.Fatal("expected the focus package documentation to be included by -match")
		}
	})
}

func TestDependencyModuleNotMarkedAsRootModule(t *testing.T) {
	// Only the modules of root and context packages are root modules.
	// Dependency packages discovered via the BFS over Imports within
	// MaxPackageDistanceFromRoot may belong to external modules; marking
	// those modules as root would classify dependency files as root-module
	// files, causing them to bypass the non-root-module simplification
	// transforms (comment stripping, function body deletion) and the
	// deletion transforms that enforce the context token budget. See
	// TheoryOfFileOrdering.
	root := t.TempDir()
	// Clear GOWORK so workspace detection relies on walking up from the
	// load directory, independent of the host environment.
	t.Setenv("GOWORK", "")

	// app module requires the sibling dep module via a local replace, so
	// the dependency is resolved without network access.
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(`module example.com/app

go 1.21

require example.com/dep v0.0.0

replace example.com/dep => ../dep
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "main.go"), []byte(`package main

import "example.com/dep"

func main() { dep.Foo() }
`), 0644); err != nil {
		t.Fatal(err)
	}

	depDir := filepath.Join(root, "dep")
	if err := os.MkdirAll(depDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(`module example.com/dep

go 1.21
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "dep.go"), []byte(`package dep

// Foo does something.
func Foo() {
	println("hello")
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)
	scope.Fork(
		func() LoadDir {
			return LoadDir(appDir)
		},
	).Call(func(
		getFiles GetFiles,
	) {
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}
		var depFile *File
		for _, f := range files {
			if f.Path == filepath.Join(depDir, "dep.go") {
				depFile = f
			}
		}
		if depFile == nil {
			t.Fatal("dependency file not loaded")
		}
		if depFile.ModuleIsRoot {
			t.Fatal("dependency module must not be marked as root module")
		}
	})
}

func TestStdLibExcludedByDefault(t *testing.T) {
	// Standard library packages are excluded from the context by default:
	// the model already knows the standard library, so its source would
	// only consume tokens from the context budget without adding
	// information. The exclusion happens at collection time in GetFiles:
	// the import walk skips packages whose Module is nil (the go/packages
	// marker for standard library), so stdlib files are never parsed,
	// token-counted, or rendered. Explicitly requesting a standard library
	// package via -pkg adds it as a focus package, because explicit user
	// intent overrides the default exclusion; its transitive standard
	// library dependencies remain excluded. See TheoryOfStdLibExclusion.
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/stdlibtest\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// main.go imports fmt and strings from the standard library; neither
	// must appear in the file set or in the assembled context parts.
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(`package main

import (
	"fmt"
	"strings"
)

func main() {
	fmt.Println(strings.ToUpper("hello"))
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	// Normalize the GOROOT path so the marker check works when GOROOT is
	// reached through a symlink.
	goroot := runtime.GOROOT()
	if resolved, err := filepath.EvalSymlinks(goroot); err == nil {
		goroot = resolved
	}
	gorootSrc := filepath.Join(goroot, "src") + string(filepath.Separator)

	scope.Fork(
		func() LoadDir { return LoadDir(root) },
	).Call(func(
		getFiles GetFiles,
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.Package != nil && f.Package.Module == nil {
				t.Fatalf("standard library file %s must not be collected by default", f.Path)
			}
		}

		parts, err := provider.Parts(1<<20, countTokens, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				continue
			}
			s := string(text)
			if strings.Contains(s, "begin of focus file "+gorootSrc) ||
				strings.Contains(s, "begin of context file "+gorootSrc) {
				t.Fatalf("standard library file must not appear in context parts:\n%s", s)
			}
		}
	})

	// Explicitly requesting a standard library package via -pkg includes it
	// as a focus package: explicit user intent overrides the default
	// exclusion, while its transitive standard library dependencies remain
	// excluded. See TheoryOfStdLibExclusion.
	scope.Fork(
		func() LoadDir { return LoadDir(root) },
		func() LoadPatterns { return LoadPatterns{"./...", "fmt"} },
	).Call(func(
		getFiles GetFiles,
	) {
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}
		var fmtFile *File
		for _, f := range files {
			if f.Package != nil && f.Package.PkgPath == "fmt" {
				fmtFile = f
				break
			}
		}
		if fmtFile == nil {
			t.Fatal("explicitly requested standard library package fmt must be included")
		}
		if !fmtFile.PackageIsRoot {
			t.Fatal("explicitly requested standard library package must be a root package")
		}
	})
}
