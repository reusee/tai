package gotools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func TestAllSrcFlag(t *testing.T) {
	flag := AllSrc(false)

	keys := flag.Keys()
	if _, ok := keys["-all-src"]; !ok {
		t.Fatalf("expected a -all-src key, got %v", keys)
	}

	newDef, remainArgs, err := flag.Handle("-all-src", nil)
	if err != nil {
		t.Fatal(err)
	}
	ptr, ok := newDef.(*AllSrc)
	if !ok || !bool(*ptr) {
		t.Fatalf("expected *AllSrc(true), got %T(%v)", newDef, newDef)
	}
	if len(remainArgs) != 0 {
		t.Fatalf("expected no remaining args, got %v", remainArgs)
	}

	paths := flag.ConfigPaths()
	if len(paths) != 1 || paths[0] != "go.all_src" {
		t.Fatalf("expected config path go.all_src, got %v", paths)
	}
}

func TestFocusPackageAllSrcContext(t *testing.T) {
	// With -all-src, focus packages are pinned at VisibilityAll: the
	// initial context carries the full source including test files, the
	// synthetic focus documentation block is not produced, and go-src
	// fetching is unnecessary for focus declarations. Non-Go focus files
	// (embed files) are emitted exactly once by the per-level loop.
	// See TheoryOfVisibilityAllocation.
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/allsrc\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "allsrc.go"), []byte(`package allsrc

import _ "embed"

//go:embed notes.txt
var notes string

// Exported returns something.
func Exported() string {
	return helper()
}

func helper() string {
	return "helper result"
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "allsrc_test.go"), []byte(`package allsrc

import "testing"

func TestExported(t *testing.T) {
	if Exported() == "" {
		t.Fatal("empty")
	}
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("focus note content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(root) },
		func() AllSrc { return AllSrc(true) },
	).Call(func(
		provider PartsProvider,
	) {
		parts, err := provider.Parts(1<<20, generators.DeepseekTokenCounterFn, nil)
		if err != nil {
			t.Fatal(err)
		}
		var context strings.Builder
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				context.WriteString(string(text))
			}
		}
		got := context.String()
		if strings.Contains(got, "begin of focus package") {
			t.Fatalf("focus documentation block must not appear under -all-src:\n%s", got)
		}
		if !strings.Contains(got, `return helper()`) {
			t.Fatalf("expected focus source bodies in the initial context:\n%s", got)
		}
		if !strings.Contains(got, `Exported() == ""`) {
			t.Fatalf("expected the focus test file body in the initial context:\n%s", got)
		}
		if n := strings.Count(got, "focus note content"); n != 1 {
			t.Fatalf("expected the non-Go focus file exactly once, got %d:\n%s", n, got)
		}
	})
}
