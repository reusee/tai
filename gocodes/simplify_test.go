package gocodes

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
)

func TestSimplify(t *testing.T) {
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
		getGenerator generators.GetGenerator,
		simplifyFiles SimplifyFiles,
	) {

		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}

		files, err = simplifyFiles(files, 256, generators.DeepseekTokenCounterFn)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) < 2 {
			t.Fatalf("got %v", len(files))
		}
		t.Logf("num files: %v", len(files))
		if files[len(files)-1].Path != filepath.Join(dir, "main.go") {
			t.Fatalf("got %v", files[0].Path)
		}
		if files[len(files)-2].Path != filepath.Join(dir, "a.txt") {
			t.Fatalf("got %v", files[0].Path)
		}
		if files[len(files)-3].Path != filepath.Join(dir, "..", "dep1", "dep1.go") {
			t.Fatalf("got %v", files[1].Path)
		}

	})
}

func TestSimplifySingleFile(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	)

	dir := t.TempDir()
	err := os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte(`
	package main

	func main() {}
		`),
		0644,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(`
module test
	`),
		0644,
	)
	if err != nil {
		t.Fatal(err)
	}

	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(8192, countTokens, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = parts
	})

}

func TestDeleteFunctionBodyPreservesDoc(t *testing.T) {
	fset := token.NewFileSet()
	const src = `package main

// This is the doc for main.
func main() {
       println("hello")
}
`
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	simplified := deleteFunctionBody(file)

	buf := new(bytes.Buffer)
	err = format.Node(buf, fset, simplified)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "// This is the doc for main.") {
		t.Errorf("doc comment was removed:\n%s", got)
	}
	if strings.Contains(got, `println("hello")`) {
		t.Errorf("function body was not removed:\n%s", got)
	}
	if !strings.Contains(got, `panic("function body omitted")`) {
		t.Errorf("function body not correctly replaced:\n%s", got)
	}
}

func TestDeleteFunctionBody(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test", `
package main

func main() {
       println("hello")
}

func foo() (int, error) {
       return 0, nil
}

func bar() {
       println("hello")
}
       `, 0)
	if err != nil {
		t.Fatal(err)
	}

	simplified := deleteFunctionBody(file)

	buf := new(bytes.Buffer)
	err = format.Node(buf, fset, simplified)
	if err != nil {
		t.Fatal(err)
	}

	text := buf.String()
	if strings.Count(text, `panic("function body omitted")`) != 3 {
		t.Fatalf("expected 3 panics, got %s", text)
	}
}

func TestCalculateMaxContextTokensCapsAt32K(t *testing.T) {
	// The function returns a fixed constant (maximumContextTokenBudget) regardless of input.
	for _, focus := range []int{0, 12 << 10, 60 << 10, 128 << 10} {
		got := calculateMaxContextTokens()
		want := maximumContextTokenBudget
		if got != want {
			t.Errorf("for focus %d: expected %d, got %d", focus, want, got)
		}
	}
}

func TestAllocateVisibilityPredecessorConstraint(t *testing.T) {
	t.Run("blocks lower priority when higher priority invisible", func(t *testing.T) {
		// Bug reproduction: when a higher-priority package cannot afford
		// any visibility level, lower-priority packages must not exceed
		// its visibility (zero). Without the predecessor constraint in
		// the minimum visibility allocation, the lower-priority package
		// gets level 1 while the higher-priority package stays at 0,
		// violating the construction principle.
		// See TheoryOfVisibilityAllocation.
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityAll,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 0, 0, 100},
			},
			{
				PkgPath:       "context",
				Category:      CategoryContext,
				MinVisibility: VisibilityCode,
				Visibility:    VisibilityInvisible,
				// Level 2 costs 50000, exceeding the 32K budget
				BudgetTokensByLevel: [4]int{0, 40000, 50000, 50000},
			},
			{
				PkgPath:       "samemodule",
				Category:      CategorySameModule,
				MinVisibility: VisibilityDoc,
				Visibility:    VisibilityInvisible,
				// Level 1 costs 50, affordable on its own
				BudgetTokensByLevel: [4]int{0, 50, 50, 50},
			},
		}

		allocateVisibility(pkgs, logs.Logger{}, false)

		if pkgs[0].Visibility != VisibilityAll {
			t.Fatalf("focus should be at level 3, got %d", pkgs[0].Visibility)
		}
		if pkgs[1].Visibility != VisibilityInvisible {
			t.Fatalf("context should be invisible, got %d", pkgs[1].Visibility)
		}
		if pkgs[2].Visibility != VisibilityInvisible {
			t.Fatalf("samemodule should be invisible (predecessor constraint), got %d", pkgs[2].Visibility)
		}
	})

	t.Run("construction principle holds when affordable", func(t *testing.T) {
		// Verify the construction principle holds when packages can afford
		// their minimum visibility: higher-priority packages have at least
		// as much visibility as lower-priority ones.
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityAll,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 0, 0, 100},
			},
			{
				PkgPath:             "context",
				Category:            CategoryContext,
				MinVisibility:       VisibilityCode,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 100, 200, 300},
			},
			{
				PkgPath:             "samemodule",
				Category:            CategorySameModule,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 50, 100, 150},
			},
		}

		allocateVisibility(pkgs, logs.Logger{}, false)

		if pkgs[0].Visibility < pkgs[1].Visibility {
			t.Fatalf("construction principle violated: focus (%d) < context (%d)",
				pkgs[0].Visibility, pkgs[1].Visibility)
		}
		if pkgs[1].Visibility < pkgs[2].Visibility {
			t.Fatalf("construction principle violated: context (%d) < samemodule (%d)",
				pkgs[1].Visibility, pkgs[2].Visibility)
		}
	})
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		pattern string
		match   bool
	}{
		{
			name:    "simple star",
			path:    "foo.go",
			pattern: "*.go",
			match:   true,
		},
		{
			name:    "simple star no match",
			path:    "foo.txt",
			pattern: "*.go",
			match:   false,
		},
		{
			name:    "star in middle",
			path:    "a/b/c.go",
			pattern: "a/*/c.go",
			match:   true,
		},
		{
			name:    "double star",
			path:    "a/b/c.go",
			pattern: "a/**/c.go",
			match:   true,
		},
		{
			name:    "double star matches zero",
			path:    "a/c.go",
			pattern: "a/**/c.go",
			match:   true,
		},
		{
			name:    "double star matches multiple",
			path:    "a/b/c/d.go",
			pattern: "a/**/d.go",
			match:   true,
		},
		{
			name:    "double star no match",
			path:    "b/c.go",
			pattern: "a/**/c.go",
			match:   false,
		},
		{
			name:    "double star prefix",
			path:    "anything/here/file.go",
			pattern: "**/file.go",
			match:   true,
		},
		{
			name:    "question mark",
			path:    "a.go",
			pattern: "?.go",
			match:   true,
		},
		{
			name:    "character class",
			path:    "abc.go",
			pattern: "[ab]bc.go",
			match:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPattern(tt.path, tt.pattern)
			if got != tt.match {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.match)
			}
		})
	}
}

func TestSimplifyContextBudgetFixed(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	)

	root := t.TempDir()
	dir := filepath.Join(root, "main")
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import "test/dep1"

func main() {
	dep1.Foo()
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.MkdirAll(filepath.Join(root, "dep1"), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(root, "dep1", "dep1.go"), []byte(`package dep1

// Foo does something.
func Foo() {
	println("hello from dep1")
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		// Simplification stops when contextTokens <= maxContextTokens
		// (32K), so small context files are never simplified, preserving
		// the LLM prefix cache.
		parts, err := provider.Parts(1, countTokens, nil)
		if err != nil {
			t.Fatalf("Parts returned error: %v", err)
		}

		var dep1Content string
		for _, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				continue
			}
			s := string(text)
			if strings.Contains(s, "dep1.go") {
				dep1Content = s
			}
		}
		if dep1Content == "" {
			t.Fatal("dep1.go not found in parts")
		}

		if strings.Contains(dep1Content, `panic("function body omitted")`) {
			t.Errorf("dep1.go was simplified despite context being within budget:\n%s", dep1Content)
		}
		if !strings.Contains(dep1Content, `println("hello from dep1")`) {
			t.Errorf("dep1.go function body was removed:\n%s", dep1Content)
		}
	})

}

func TestPackagesLoadOmitsNeedTypes(t *testing.T) {
	// The loader must not request NeedTypes, otherwise packages.Load
	// type-checks the dependency graph and OOMs on large trees.
	// NeedDeps is used (dependencies are loaded) but NeedTypes is omitted
	// (no type checking), so memory stays bounded. Assert that Types and
	// TypesInfo remain nil after a successful load against the real module
	// wiring.
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
		getRoot GetRootPackages,
	) {
		pkgs, err := getRoot()
		if err != nil {
			t.Fatal(err)
		}
		if len(pkgs) == 0 {
			t.Fatal("expected at least one root package")
		}
		// NeedTypes is offline: Types stays nil; TypesInfo stays nil.
		// NeedDeps loads dependencies but NeedTypes is omitted so no
		// type checking occurs, keeping memory bounded.
		for _, pkg := range pkgs {
			if pkg.Types != nil {
				t.Fatalf("pkg.Types must be nil without NeedTypes, got non-nil for %s", pkg.PkgPath)
			}
			if pkg.TypesInfo != nil {
				t.Fatalf("pkg.TypesInfo must be nil without NeedTypesInfo, got non-nil for %s", pkg.PkgPath)
			}
			// GoFiles must still be available for free file discovery.
			if len(pkg.GoFiles) == 0 {
				t.Fatalf("pkg.GoFiles must be populated via NeedFiles for %s", pkg.PkgPath)
			}
		}
	})
}
