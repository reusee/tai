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
		dscope.Provide(configs.NewLoader(nil, "")),
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

		first := files[len(files)-1]
		if first.Path != filepath.Join(dir, "main.go") {
			t.Fatalf("got %v", first.Path)
		}
		if first.TokenFile == nil {
			t.Fatal()
		}
		if first.AstFile == nil {
			t.Fatal()
		}
		if first.Package == nil {
			t.Fatal()
		}
		if !first.PackageIsRoot {
			t.Fatal()
		}
		if first.PackageDistanceFromRoot != 0 {
			t.Fatal()
		}
		if first.Module == nil {
			t.Fatal()
		}
		if !first.ModuleIsRoot {
			t.Fatal()
		}

		second := files[len(files)-2]
		if second.Path != filepath.Join(dir, "a.txt") {
			t.Fatalf("got %v", second.Path)
		}
		if second.Package == nil {
			t.Fatal()
		}
		if !second.PackageIsRoot {
			t.Fatal()
		}
		if second.PackageDistanceFromRoot != 0 {
			t.Fatalf("got %v", second.PackageDistanceFromRoot)
		}
		if second.Module == nil {
			t.Fatal()
		}
		if !second.ModuleIsRoot {
			t.Fatal()
		}

		third := files[len(files)-3]
		if third.Path != filepath.Join(dir, "..", "dep1", "dep1.go") {
			t.Fatalf("got %v", third.Path)
		}
		if third.TokenFile == nil {
			t.Fatal()
		}
		if third.AstFile == nil {
			t.Fatal()
		}
		if third.Package == nil {
			t.Fatal()
		}
		if third.PackageIsRoot {
			t.Fatal()
		}
		if third.PackageDistanceFromRoot != 1 {
			t.Fatal()
		}
		if third.Module == nil {
			t.Fatal()
		}
		if !third.ModuleIsRoot {
			t.Fatal()
		}

	})

}
