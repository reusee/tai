package gocodes

import (
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
		if mainFile.PackageDistanceFromRoot != 0 {
			t.Errorf("main.go distance %d, want 0", mainFile.PackageDistanceFromRoot)
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
		if aTxtFile.PackageDistanceFromRoot != 0 {
			t.Errorf("a.txt distance %d, want 0", aTxtFile.PackageDistanceFromRoot)
		}
		if aTxtFile.Module == nil {
			t.Error("a.txt Module is nil")
		}
		if !aTxtFile.ModuleIsRoot {
			t.Error("a.txt not marked as root module")
		}

		// dep1.go checks
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
		if dep1File.PackageDistanceFromRoot != 1 {
			t.Errorf("dep1.go distance %d, want 1", dep1File.PackageDistanceFromRoot)
		}
		if dep1File.Module == nil {
			t.Error("dep1.go Module is nil")
		}
		if !dep1File.ModuleIsRoot {
			t.Error("dep1.go not marked as root module")
		}

	})

}
