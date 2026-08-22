package gotools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestNewHiddenPackageMatcher(t *testing.T) {
	matcher := newHiddenPackageMatcher([]string{
		"example.com/foo/bar/...",
		"example.com/baz",
		"  ",
	})
	if matcher == nil {
		t.Fatal("non-empty pattern set must produce a matcher")
	}

	for _, path := range []string{
		"example.com/foo/bar",
		"example.com/foo/bar/sub",
		"example.com/foo/bar/sub/deep",
		"example.com/baz",
		"example.com/baz [baz.test]",
	} {
		if !matcher(path) {
			t.Errorf("pattern set should hide %q", path)
		}
	}

	for _, path := range []string{
		"example.com/foo",
		"example.com/foo/barbar",
		"example.com/baz/sub",
		"example.com/other",
		"other.com/baz",
	} {
		if matcher(path) {
			t.Errorf("pattern set must not hide %q", path)
		}
	}

	if newHiddenPackageMatcher(nil) != nil {
		t.Error("no patterns must produce a nil matcher")
	}
	if newHiddenPackageMatcher([]string{"", " "}) != nil {
		t.Error("whitespace-only patterns must produce a nil matcher")
	}
}

// TestHiddenPackagesExcludeFilesAndDocs verifies end to end that a
// go.hidden pattern removes the matched packages' files from the loaded
// file set and their documentation from the simplified output, while
// non-matching packages keep their focus documentation. See
// TheoryOfHiddenPackages.
func TestHiddenPackagesExcludeFilesAndDocs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hiddentest\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writePkg := func(rel, name string) string {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".go"), []byte("package "+name+"\n\nfunc "+name+"() {}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	fooDir := writePkg("foo", "foo")
	barDir := writePkg("bar", "bar")
	subDir := writePkg("bar/sub", "sub")

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(root) },
		func() HiddenPatterns { return HiddenPatterns{"example.com/hiddentest/bar/..."} },
	).Call(func(
		getFiles GetFiles,
		simplifyFiles SimplifyFiles,
	) {
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}
		var fooFile, barFile, subFile *File
		for _, f := range files {
			switch f.Path {
			case filepath.Join(fooDir, "foo.go"):
				fooFile = f
			case filepath.Join(barDir, "bar.go"):
				barFile = f
			case filepath.Join(subDir, "sub.go"):
				subFile = f
			}
		}
		if fooFile == nil {
			t.Fatal("foo.go must be loaded: foo is not hidden")
		}
		if barFile != nil {
			t.Fatal("bar.go must not be loaded: bar is hidden")
		}
		if subFile != nil {
			t.Fatal("bar/sub sub.go must not be loaded: bar/... hides subpackages")
		}

		simplified, err := simplifyFiles(files, 32<<10, func(s string) (int, error) {
			return len(s) / 4, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var fooDoc bool
		for _, f := range simplified {
			if f.Package != nil && strings.HasPrefix(f.Package.PkgPath, "example.com/hiddentest/bar") {
				t.Errorf("hidden package %q must not appear in simplified output", f.Package.PkgPath)
			}
			switch f.Path {
			case "example.com/hiddentest/foo":
				fooDoc = true
			case "example.com/hiddentest/bar", "example.com/hiddentest/bar/sub":
				t.Errorf("hidden package documentation %q must not be emitted", f.Path)
			}
		}
		if !fooDoc {
			t.Fatal("focus package foo documentation must be emitted")
		}
	})
}
