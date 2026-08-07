package gocodes

import (
	"bytes"
	"log/slog"
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

func TestCalculateMaxContextTokensDynamic(t *testing.T) {
	// The context token budget scales with focus package size: focus
	// tokens / 2, rounded to the nearest multiple of
	// contextTokenBudgetUnit, with a floor at one unit. See
	// TheoryOfVisibilityAllocation.
	tests := []struct {
		focusTokens int
		want        int
	}{
		{0, contextTokenBudgetUnit},        // half=0 → rounds to 0 → floor at one unit
		{12 << 10, contextTokenBudgetUnit}, // half=6K → rounds to 0 → floor at one unit
		{60 << 10, contextTokenBudgetUnit}, // half=30K → rounds to 32K
		{64 << 10, contextTokenBudgetUnit}, // half=32K → exactly 32K
		{100 << 10, 64 << 10},              // half=50K → rounds to 64K
		{128 << 10, 64 << 10},              // half=64K → exactly 64K
		{200 << 10, 96 << 10},              // half=100K → rounds to 96K
	}
	for _, tt := range tests {
		got := calculateMaxContextTokens(tt.focusTokens)
		if got != tt.want {
			t.Errorf("for focus %d: expected %d, got %d", tt.focusTokens, tt.want, got)
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

		allocateVisibility(pkgs, logs.Logger{}, false, nil)

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

		allocateVisibility(pkgs, logs.Logger{}, false, nil)

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

func TestAllocateVisibilityLazyDocComputation(t *testing.T) {
	// Package documentation (go doc output) is computed lazily, only for
	// packages that actually reach visibility level 1. The eager approach
	// ran go doc for every non-focus package in the dependency graph — one
	// Go toolchain subprocess per package — even though most packages end
	// at level 0 (invisible) or at levels 2/3 (full code), where the doc
	// output is never used. See TheoryOfLazyPackageDoc.

	t.Run("ComputesDocForLevelOnePackages", func(t *testing.T) {
		var docCalls []string
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityAll,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 0, 0, 100},
			},
			{
				// Same-module package whose doc fits the budget but whose
				// full code (level 2) does not: it lands at level 1, so
				// its doc is computed exactly once.
				PkgPath:             "samemodule",
				Category:            CategorySameModule,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 100, 1 << 30, 1 << 30},
			},
		}
		computeDoc := func(lp *LogicalPackage) {
			if lp.docComputed {
				return
			}
			docCalls = append(docCalls, lp.PkgPath)
			lp.DocContent = "doc"
			lp.DocTokens = 100
			lp.BudgetTokensByLevel[VisibilityDoc] = 100
			lp.TokensByLevel[VisibilityDoc] = 100
			lp.docComputed = true
		}
		allocateVisibility(pkgs, logs.Logger{}, false, computeDoc)

		if len(docCalls) != 1 || docCalls[0] != "samemodule" {
			t.Fatalf("expected doc computed once for samemodule, got %v", docCalls)
		}
		if pkgs[1].Visibility != VisibilityDoc {
			t.Fatalf("expected samemodule at level 1, got %d", pkgs[1].Visibility)
		}
	})

	t.Run("SkipsDocForPackagesNeverAtLevelOne", func(t *testing.T) {
		var docCalls []string
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityAll,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 0, 0, 100},
			},
			{
				// Context package whose doc AND code costs exceed the
				// budget: it stays invisible, capping every subsequent
				// package at level 0. It is probed once for the 0→1
				// upgrade — the single unavoidable go doc invocation
				// needed to learn its real doc cost — and the docComputed
				// guard prevents repeated probes on later water-fill
				// iterations.
				PkgPath:             "context",
				Category:            CategoryContext,
				MinVisibility:       VisibilityCode,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 60000, 60000, 60000},
			},
			{
				// Capped to level 0 by the predecessor constraint in the
				// min-visibility pass, and blocked from the 0→1 upgrade
				// in the water-fill phase: its doc is never needed. The
				// eager approach would have run go doc for this package.
				PkgPath:             "samemodule",
				Category:            CategorySameModule,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 50, 50, 50},
			},
		}
		computeDoc := func(lp *LogicalPackage) {
			if lp.docComputed {
				return
			}
			docCalls = append(docCalls, lp.PkgPath)
			// The fake doc cost equals the pre-set cost, so the
			// allocation behaves as if the doc were pre-computed.
			lp.docComputed = true
		}
		allocateVisibility(pkgs, logs.Logger{}, false, computeDoc)

		// samemodule never reaches level 1 and must never have its doc
		// computed.
		for _, p := range docCalls {
			if p == "samemodule" {
				t.Fatalf("doc must not be computed for a package capped at level 0: %v", docCalls)
			}
		}
		// context is probed exactly once; the docComputed guard prevents
		// repeated go doc invocations on subsequent water-fill iterations.
		if len(docCalls) != 1 || docCalls[0] != "context" {
			t.Fatalf("expected one doc probe for context, got %v", docCalls)
		}
		if pkgs[2].Visibility != VisibilityInvisible {
			t.Fatalf("expected samemodule invisible (predecessor constraint), got %d", pkgs[2].Visibility)
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

func TestLogTokenComposition(t *testing.T) {
	// The context token composition log must report focus package
	// tokens, the dynamic context budget, and how the context packages
	// consume that budget by visibility level. See TheoryOfTokenComposition.
	pkgs := []*LogicalPackage{
		{
			PkgPath:       "focus",
			Category:      CategoryFocus,
			Visibility:    VisibilityAll,
			TokensByLevel: [4]int{0, 0, 0, 500},
		},
		{
			PkgPath:       "docpkg",
			Category:      CategorySameModule,
			Visibility:    VisibilityDoc,
			TokensByLevel: [4]int{0, 100, 300, 400},
		},
		{
			PkgPath:       "codepkg",
			Category:      CategoryContext,
			Visibility:    VisibilityCode,
			TokensByLevel: [4]int{0, 50, 200, 300},
		},
		{
			PkgPath:       "fullpkg",
			Category:      CategorySameModule,
			Visibility:    VisibilityAll,
			TokensByLevel: [4]int{0, 10, 20, 600},
		},
		{
			PkgPath:       "hiddenpkg",
			Category:      CategoryStdLib,
			Visibility:    VisibilityInvisible,
			TokensByLevel: [4]int{0, 30, 40, 50},
		},
	}

	var buf bytes.Buffer
	logger := logs.Logger{slog.New(slog.NewTextHandler(&buf, nil))}
	logTokenComposition(logger, pkgs)
	output := buf.String()

	for _, want := range []string{
		`msg="context token composition"`,
		`"focus tokens"=500`,
		`"context budget"=32768`,
		`"context tokens"=900`,
		`"doc packages"=1`,
		`"code packages"=1`,
		`"full packages"=1`,
		`"invisible packages"=1`,
		`"doc tokens"=100`,
		`"code tokens"=200`,
		`"full tokens"=600`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in composition log, got: %s", want, output)
		}
	}
}

func TestSimplifyContextBudget(t *testing.T) {
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
		// Simplification stops when the context fits within the dynamic
		// context token budget, so small context files are never
		// simplified, preserving the LLM prefix cache. With the small
		// focus package in this test, the budget is the 32K floor.
		// See TheoryOfVisibilityAllocation.
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
