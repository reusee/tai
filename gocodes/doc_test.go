package gocodes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func TestDocPatternsHandleReturnsPointer(t *testing.T) {
	f := DocPatterns(nil)
	newDef, remainArgs, err := f.Handle("-doc", []string{"fmt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(remainArgs) != 0 {
		t.Fatalf("expected no remaining args, got %v", remainArgs)
	}
	ret, ok := newDef.(*DocPatterns)
	if !ok {
		t.Fatalf("expected *DocPatterns, got %T", newDef)
	}
	if len(*ret) != 1 || (*ret)[0] != "fmt" {
		t.Fatalf("unexpected DocPatterns: %v", *ret)
	}
}

func TestDocPatternsAccumulatesAcrossInvocations(t *testing.T) {
	f := DocPatterns(nil)
	newDef, _, err := f.Handle("-doc", []string{"fmt"})
	if err != nil {
		t.Fatal(err)
	}
	ret := newDef.(*DocPatterns)
	newDef, _, err = ret.Handle("-doc", []string{"os"})
	if err != nil {
		t.Fatal(err)
	}
	ret = newDef.(*DocPatterns)
	if len(*ret) != 2 || (*ret)[0] != "fmt" || (*ret)[1] != "os" {
		t.Fatalf("unexpected DocPatterns: %v", *ret)
	}
}

func TestCodeProviderIncludesPackageDoc(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/docpkg\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(root, "mypkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "mypkg.go"), []byte(`// Package mypkg demonstrates documentation.
package mypkg

// Foo returns the value 42.
func Foo() int { return 42 }
`), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	).Fork(
		func() LoadDir { return LoadDir(root) },
		func() DocPatterns { return DocPatterns{"example.com/docpkg/mypkg"} },
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(1<<20, countTokens, nil)
		if err != nil {
			t.Fatalf("provider.Parts failed: %v", err)
		}
		found := false
		for _, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				continue
			}
			s := string(text)
			if strings.Contains(s, "begin of context package example.com/docpkg/mypkg") {
				found = true
				if !strings.Contains(s, "Package mypkg demonstrates documentation") {
					t.Fatalf("package doc block must contain the package documentation:\n%s", s)
				}
			}
		}
		if !found {
			t.Fatal("expected package doc in context parts")
		}
	})
}

func TestCodeProviderDocErrorSurfaces(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/docpkg\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(root, "mypkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "mypkg.go"), []byte("package mypkg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	).Fork(
		func() LoadDir { return LoadDir(root) },
		func() DocPatterns { return DocPatterns{"example.com/docpkg/nonexistent"} },
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		_, err := provider.Parts(1<<20, countTokens, nil)
		if err == nil {
			t.Fatal("expected error for nonexistent doc package")
		}
		if !strings.Contains(err.Error(), "go doc") {
			t.Fatalf("expected error to mention go doc, got: %v", err)
		}
	})
}

func TestRenderPackageDocWithoutUnexported(t *testing.T) {
	// renderPackageDoc invokes go doc without the -u flag, so unexported
	// symbols must not appear in the rendered documentation. With -u the
	// output roughly doubles for most packages without adding API-level
	// reference value.
	root := t.TempDir()
	t.Setenv("GOWORK", "")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/docpkg\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg.go"), []byte(`// Package docpkg demonstrates documentation.
package docpkg

// Foo returns 42.
func Foo() int { return 42 }

// helper does something unexported.
func helper() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	content, _, err := renderPackageDoc(
		"example.com/docpkg",
		root,
		withModModEnv(os.Environ()),
		generators.DeepseekTokenCounterFn,
	)
	if err != nil {
		t.Fatalf("renderPackageDoc failed: %v", err)
	}
	if !strings.Contains(content, "Foo") {
		t.Fatalf("documentation must include the exported symbol Foo:\n%s", content)
	}
	if strings.Contains(content, "helper") {
		t.Fatalf("documentation must not include unexported symbols (the -u flag is removed):\n%s", content)
	}
}
