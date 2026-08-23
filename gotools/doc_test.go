package gotools

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
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

func TestPartsProviderIncludesPackageDoc(t *testing.T) {
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
	).Fork(
		func() LoadDir { return LoadDir(root) },
		func() DocPatterns { return DocPatterns{"example.com/docpkg/mypkg"} },
	).Call(func(
		provider PartsProvider,
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

func TestPartsProviderDocErrorSurfaces(t *testing.T) {
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
	).Fork(
		func() LoadDir { return LoadDir(root) },
		func() DocPatterns { return DocPatterns{"example.com/docpkg/nonexistent"} },
	).Call(func(
		provider PartsProvider,
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

func TestRenderShortDoc(t *testing.T) {
	// The short-doc visibility level renders go doc without -all: the
	// package overview and the top-level symbol index, without per-symbol
	// documentation. It must be strictly smaller than the full
	// documentation and must carry the context package markers. See
	// TheoryOfLazyPackageDoc.
	root := t.TempDir()
	t.Setenv("GOWORK", "")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shortdoc\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shortdoc.go"), []byte(`// Package shortdoc demonstrates short documentation.
package shortdoc

// FooMarker returns 42 with a long comment attached to the declaration so
// the full documentation carries text that the short documentation omits.
func FooMarker() int { return 42 }

// BarMarker does something else.
func BarMarker() int { return 43 }
`), 0644); err != nil {
		t.Fatal(err)
	}

	shortContent, shortTokens, err := renderShortDoc(
		"example.com/shortdoc",
		root,
		withModModEnv(os.Environ()),
		generators.DeepseekTokenCounterFn,
	)
	if err != nil {
		t.Fatalf("renderShortDoc failed: %v", err)
	}

	fullContent, fullTokens, err := renderPackageDoc(
		"example.com/shortdoc",
		root,
		withModModEnv(os.Environ()),
		generators.DeepseekTokenCounterFn,
	)
	if err != nil {
		t.Fatalf("renderPackageDoc failed: %v", err)
	}

	if !strings.Contains(shortContent, "begin of context package example.com/shortdoc") {
		t.Fatalf("short doc must carry the context package markers:\n%s", shortContent)
	}
	if !strings.Contains(shortContent, "FooMarker") {
		t.Fatalf("short doc must list the top-level symbol index:\n%s", shortContent)
	}
	if strings.Contains(shortContent, "long comment attached") {
		t.Fatalf("short doc must omit per-symbol documentation (no -all):\n%s", shortContent)
	}
	if !strings.Contains(fullContent, "long comment attached") {
		t.Fatalf("full doc must include per-symbol documentation:\n%s", fullContent)
	}
	if shortTokens >= fullTokens {
		t.Fatalf("short doc tokens (%d) must be smaller than full doc tokens (%d)", shortTokens, fullTokens)
	}
}

func TestRenderPackageDocDoesNotModifyGoSum(t *testing.T) {
	// go doc must not modify go.sum: the load environment injects
	// -mod=mod so go list can update go.mod when it is out of sync, but
	// go doc would then re-add checksums that go mod tidy removed,
	// causing go.sum to churn. See TheoryOfGoDocReadonly.

	// Serve example.com/dep v1.0.0 from a file-based module proxy.
	proxyDir := t.TempDir()
	depProxyDir := filepath.Join(proxyDir, "example.com", "dep", "@v")
	if err := os.MkdirAll(depProxyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depProxyDir, "list"), []byte("v1.0.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depProxyDir, "v1.0.0.info"), []byte(`{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depProxyDir, "v1.0.0.mod"), []byte("module example.com/dep\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	for name, content := range map[string]string{
		"go.mod": "module example.com/dep\n\ngo 1.21\n",
		"dep.go": "package dep\n\n// Foo does something.\nfunc Foo() {}\n",
	} {
		w, err := zw.Create("example.com/dep@v1.0.0/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depProxyDir, "v1.0.0.zip"), zipBuf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	// Main module requiring the dep.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/main\n\ngo 1.21\n\nrequire example.com/dep v1.0.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nimport \"example.com/dep\"\n\nfunc main() { dep.Foo() }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Isolated module cache and proxy environment.
	cacheDir := t.TempDir()
	// The go command makes extracted module directories read-only
	// (0555), which would make t.TempDir's cleanup fail with permission
	// denied. Restore write permission on the cache tree before the
	// temp dir is removed.
	t.Cleanup(func() {
		filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				os.Chmod(path, 0755)
			}
			return nil
		})
	})
	setEnv := func(envs []string, key, value string) []string {
		prefix := key + "="
		for i, e := range envs {
			if strings.HasPrefix(e, prefix) {
				envs[i] = prefix + value
				return envs
			}
		}
		return append(envs, prefix+value)
	}
	envs := setEnv(os.Environ(), "GOPROXY", "file://"+proxyDir)
	envs = setEnv(envs, "GOSUMDB", "off")
	envs = setEnv(envs, "GOMODCACHE", cacheDir)
	envs = setEnv(envs, "GOWORK", "off")
	envs = setEnv(envs, "GOFLAGS", "")
	envs = setEnv(envs, "GOPRIVATE", "")
	envs = setEnv(envs, "GONOPROXY", "")

	// Populate go.mod and go.sum.
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = root
	cmd.Env = envs
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}

	// The dep is loadable with a complete go.sum.
	cmd = exec.Command("go", "doc", "example.com/dep")
	cmd.Dir = root
	cmd.Env = envs
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go doc: %v\n%s", err, out)
	}

	// Remove the dep's go.sum entries, simulating go mod tidy removing
	// checksums that go doc later needs.
	goSumPath := filepath.Join(root, "go.sum")
	goSum, err := os.ReadFile(goSumPath)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(string(goSum), "\n") {
		if strings.HasPrefix(line, "example.com/dep ") {
			continue
		}
		kept = append(kept, line)
	}
	trimmedGoSum := strings.Join(kept, "\n")
	if err := os.WriteFile(goSumPath, []byte(trimmedGoSum), 0644); err != nil {
		t.Fatal(err)
	}

	// renderPackageDoc must not modify go.sum: with -mod=readonly, go
	// doc fails on the missing checksum instead of re-adding it.
	docEnv := setEnv(append([]string(nil), envs...), "GOFLAGS", "-mod=mod")
	_, _, err = renderPackageDoc("example.com/dep", root, docEnv, generators.DeepseekTokenCounterFn)
	if err == nil {
		t.Fatal("expected go doc to fail with a missing go.sum entry")
	}
	after, err := os.ReadFile(goSumPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != trimmedGoSum {
		t.Fatalf("go doc modified go.sum:\nbefore: %q\nafter: %q", trimmedGoSum, string(after))
	}
}
