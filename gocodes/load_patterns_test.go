package gocodes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestLoadPatternsHandleReplacesDefault(t *testing.T) {
	// The default load patterns is ["./..."], which loads every package
	// in the current directory as a focus package. An explicit -pkg flag
	// must replace this default so that focus is limited to the
	// specified packages; appending would keep ./... and load every
	// package in the current directory as focus. See
	// TheoryOfDefaultLoadPattern.
	f := LoadPatterns{DefaultLoadPattern}
	newDef, remainArgs, err := f.Handle("-pkg", []string{"./foo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(remainArgs) != 0 {
		t.Fatalf("expected no remaining args, got %v", remainArgs)
	}
	ret, ok := newDef.(*LoadPatterns)
	if !ok {
		t.Fatalf("expected *LoadPatterns, got %T", newDef)
	}
	if len(*ret) != 1 || (*ret)[0] != "./foo" {
		t.Fatalf("expected [./foo], got %v", *ret)
	}
}

func TestLoadPatternsHandleAccumulatesAcrossInvocations(t *testing.T) {
	// Multiple -pkg flags accumulate after the default has been replaced
	// by the first explicit pattern.
	f := LoadPatterns{DefaultLoadPattern}
	for _, pattern := range []string{"./foo", "./bar"} {
		newDef, _, err := f.Handle("-pkg", []string{pattern})
		if err != nil {
			t.Fatal(err)
		}
		f = *newDef.(*LoadPatterns)
	}
	if len(f) != 2 || f[0] != "./foo" || f[1] != "./bar" {
		t.Fatalf("expected [./foo ./bar], got %v", f)
	}
}

func TestLoadPatternsHandleAccumulatesOnNonDefault(t *testing.T) {
	// A non-default pattern list (e.g., from a config file) is appended
	// to: only the exact default [./...] is replaced.
	f := LoadPatterns{"./foo"}
	newDef, _, err := f.Handle("-pkg", []string{"./bar"})
	if err != nil {
		t.Fatal(err)
	}
	ret := newDef.(*LoadPatterns)
	if len(*ret) != 2 || (*ret)[0] != "./foo" || (*ret)[1] != "./bar" {
		t.Fatalf("expected [./foo ./bar], got %v", *ret)
	}
}

func TestLoadPatternsLimitsFocusPackages(t *testing.T) {
	// End-to-end: -pkg ./foo must limit focus packages to ./foo only.
	// The default ./... is replaced by the first explicit -pkg flag, so
	// a sibling package (bar) is not loaded as a focus package. See
	// TheoryOfDefaultLoadPattern.
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/limittest\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fooDir := filepath.Join(root, "foo")
	if err := os.MkdirAll(fooDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fooDir, "foo.go"), []byte("package foo\n\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	barDir := filepath.Join(root, "bar")
	if err := os.MkdirAll(barDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(barDir, "bar.go"), []byte("package bar\n\nfunc Bar() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate the flag flow: the scope default is ./..., and -pkg ./foo
	// replaces it.
	def := LoadPatterns{DefaultLoadPattern}
	newDef, _, err := def.Handle("-pkg", []string{"./foo"})
	if err != nil {
		t.Fatal(err)
	}
	patterns := newDef.(*LoadPatterns)

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(root) },
		func() LoadPatterns { return *patterns },
	).Call(func(
		getFiles GetFiles,
	) {
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}
		var fooFile, barFile *File
		for _, f := range files {
			switch f.Path {
			case filepath.Join(fooDir, "foo.go"):
				fooFile = f
			case filepath.Join(barDir, "bar.go"):
				barFile = f
			}
		}
		if fooFile == nil {
			t.Fatal("foo.go not loaded")
		}
		if !fooFile.PackageIsRoot {
			t.Fatal("foo.go should be a root package")
		}
		if barFile != nil {
			t.Fatal("bar.go must not be loaded when -pkg ./foo limits focus to foo")
		}
	})
}
