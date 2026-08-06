package gocodes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
)

func TestFiles(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
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
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
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
