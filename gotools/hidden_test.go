package gotools

import (
	"os"
	"path/filepath"
	"slices"
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

func TestHiddenPackagesSystemPrompt(t *testing.T) {
	if got := HiddenPackagesSystemPrompt(nil); got != "" {
		t.Errorf("no patterns must produce an empty prompt, got %q", got)
	}
	if got := HiddenPackagesSystemPrompt(HiddenPatterns{"", "  "}); got != "" {
		t.Errorf("whitespace-only patterns must produce an empty prompt, got %q", got)
	}

	got := HiddenPackagesSystemPrompt(HiddenPatterns{
		"example.com/baz",
		"example.com/foo/bar/...",
		"example.com/baz",
		"  example.com/qux  ",
	})
	for _, want := range []string{
		"Hidden Packages",
		"example.com/foo/bar/...",
		"example.com/baz",
		"example.com/qux",
		"go-src",
		"ingest blocks",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt must mention %q", want)
		}
	}

	// Equal pattern sets must produce byte-identical prompts regardless
	// of input order and duplication, preserving the LLM prefix cache.
	again := HiddenPackagesSystemPrompt(HiddenPatterns{
		"example.com/qux",
		"example.com/baz",
		"example.com/foo/bar/...",
		"example.com/baz",
	})
	if got != again {
		t.Error("equal pattern sets must produce byte-identical prompts")
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

// TestHiddenPackagesBlockGoSrcFallback verifies that the go-src go doc
// fallback for unloaded import paths never probes a hidden package: both
// the package path and a symbol inside it keep the plain not-found
// report, and the hidden documentation never reaches the context.
// See TheoryOfHiddenPackages.
func TestHiddenPackagesBlockGoSrcFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOWORK", "")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hiddensrc\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root.go"), []byte("package root\n"), 0644); err != nil {
		t.Fatal(err)
	}
	barDir := filepath.Join(root, "bar")
	if err := os.MkdirAll(barDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(barDir, "bar.go"), []byte("// Package bar is hidden.\npackage bar\n\n// Baz is a declaration of the hidden package.\nfunc Baz() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(root) },
		func() LoadPatterns { return LoadPatterns{"."} },
		func() HiddenPatterns { return HiddenPatterns{"example.com/hiddensrc/bar"} },
	).Call(func(resolve ResolveGoSymbols) {
		parts, err := resolve([]string{"example.com/hiddensrc/bar"})
		if err != nil {
			t.Fatal(err)
		}
		got := partsText(t, parts)
		if !strings.Contains(got, "not found") {
			t.Fatalf("expected not-found for a hidden import path, got:\n%s", got)
		}
		if strings.Contains(got, "Package bar is hidden") {
			t.Fatalf("hidden package documentation must not leak through the go-src fallback, got:\n%s", got)
		}

		// The import-path prefix of a symbol expression is checked
		// before any probe, so a symbol inside the hidden package is
		// blocked like the package path itself.
		parts, err = resolve([]string{"example.com/hiddensrc/bar.Baz"})
		if err != nil {
			t.Fatal(err)
		}
		if got = partsText(t, parts); !strings.Contains(got, "not found") {
			t.Fatalf("expected not-found for a symbol inside a hidden package, got:\n%s", got)
		}
		if strings.Contains(got, "Package bar is hidden") {
			t.Fatalf("hidden package documentation must not leak for a symbol inside it, got:\n%s", got)
		}
	})
}

// TestUnhidePatternsForWorkingDirectory verifies the working-directory
// exemption: a pattern whose base package directory contains the process
// working directory is dropped, while patterns for other directories of
// the same module and patterns of other modules stay hidden. See
// TheoryOfHiddenPackages.
func TestUnhidePatternsForWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hiddentest\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pkgA := filepath.Join(root, "pkgA")
	pkgB := filepath.Join(root, "pkgB")
	for _, dir := range []string{pkgA, pkgB} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	patterns := []string{
		"example.com/hiddentest/pkgA",
		"example.com/hiddentest/pkgA/...",
		"example.com/hiddentest/pkgB",
		"other.com/elsewhere/...",
	}

	// At the module root nothing is dropped: the root is the parent of
	// the hidden package directories, not inside them.
	t.Chdir(root)
	kept := unhidePatternsForWorkingDirectory(patterns)
	if !slices.Equal(kept, patterns) {
		t.Errorf("module root must keep every pattern, got %v", kept)
	}

	// Inside pkgA both pkgA patterns are dropped; pkgB and the other
	// module stay hidden even though they share the module.
	t.Chdir(pkgA)
	kept = unhidePatternsForWorkingDirectory(patterns)
	want := []string{
		"example.com/hiddentest/pkgB",
		"other.com/elsewhere/...",
	}
	if !slices.Equal(kept, want) {
		t.Errorf("inside pkgA must keep only %v, got %v", want, kept)
	}

	// Inside pkgB only the pkgB pattern is dropped.
	t.Chdir(pkgB)
	kept = unhidePatternsForWorkingDirectory(patterns)
	want = []string{
		"example.com/hiddentest/pkgA",
		"example.com/hiddentest/pkgA/...",
		"other.com/elsewhere/...",
	}
	if !slices.Equal(kept, want) {
		t.Errorf("inside pkgB must keep only %v, got %v", want, kept)
	}

	// Without a module above the working directory, nothing is dropped.
	t.Chdir(t.TempDir())
	kept = unhidePatternsForWorkingDirectory(patterns)
	if !slices.Equal(kept, patterns) {
		t.Errorf("no module must keep every pattern, got %v", kept)
	}
}

// TestHiddenPackagesSystemPromptUnhidesWorkingDirectory verifies that the
// system prompt omits a pattern whose base package directory contains the
// working directory, while keeping the other patterns. See
// TheoryOfHiddenPackages.
func TestHiddenPackagesSystemPromptUnhidesWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hiddentest\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pkgA := filepath.Join(root, "pkgA")
	if err := os.MkdirAll(pkgA, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(pkgA)
	got := HiddenPackagesSystemPrompt(HiddenPatterns{
		"example.com/hiddentest/pkgA",
		"example.com/hiddentest/pkgB",
	})
	if strings.Contains(got, "example.com/hiddentest/pkgA") {
		t.Error("prompt must omit the pattern of the working directory's package")
	}
	if !strings.Contains(got, "example.com/hiddentest/pkgB") {
		t.Error("prompt must keep the pattern of another package in the module")
	}
}
