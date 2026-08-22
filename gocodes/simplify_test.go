package gocodes

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
)

func TestSimplify(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
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
		// Focus packages are pinned at documentation level: the output is
		// the dep1 context file, the focus package's go doc block, and
		// the non-Go focus file a.txt (the only PackageIsRoot entry, so
		// it sorts last). main.go's full content must not appear.
		// See TheoryOfVisibilityAllocation.
		if len(files) < 3 {
			t.Fatalf("got %v", len(files))
		}
		t.Logf("num files: %v", len(files))
		var foundDep1, foundFocusDoc, foundATxt bool
		for _, f := range files {
			switch f.Path {
			case filepath.Join(dir, "..", "dep1", "dep1.go"):
				foundDep1 = true
				if !strings.Contains(f.Confirmed.What, "visibility level") {
					t.Fatalf("dep1.go should be at a code visibility level, got %q", f.Confirmed.What)
				}
			case filepath.Join(dir, "a.txt"):
				foundATxt = true
			case filepath.Join(dir, "main.go"):
				t.Fatalf("main.go must not appear at full content; focus packages are documentation-only")
			}
			if strings.Contains(f.Confirmed.What, "focus go doc -u") {
				foundFocusDoc = true
				if !strings.Contains(string(f.Confirmed.Content), "begin of focus package") {
					t.Fatalf("focus documentation block missing its marker:\n%s", f.Confirmed.Content)
				}
			}
		}
		if !foundDep1 {
			t.Fatal("dep1.go not found in output")
		}
		if !foundFocusDoc {
			t.Fatal("focus package documentation block not found in output")
		}
		if !foundATxt {
			t.Fatal("a.txt not found in output")
		}
		if files[len(files)-1].Path != filepath.Join(dir, "a.txt") {
			t.Fatalf("a.txt should be the last output file, got %v", files[len(files)-1].Path)
		}

	})
}

func TestFocusPackageDocumentationContext(t *testing.T) {
	// Focus packages are pinned at documentation level: the initial
	// context carries go doc -all -cmd -u output (unexported symbols
	// included) plus the package's test-function names, and the model
	// fetches implementation source on demand with go-src blocks. Non-Go
	// focus files stay at full content. See TheoryOfVisibilityAllocation.
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/focusdoc\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "focusdoc.go"), []byte(`package focusdoc

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
	if err := os.WriteFile(filepath.Join(root, "focusdoc_test.go"), []byte(`package focusdoc

import "testing"

func TestExported(t *testing.T) {
	if Exported() == "" {
		t.Fatal("empty")
	}
}

func BenchmarkExported(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Exported()
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
	).Call(func(
		provider CodeProvider,
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
		if !strings.Contains(got, "begin of focus package example.com/focusdoc") {
			t.Fatalf("expected the focus package documentation block:\n%s", got)
		}
		if !strings.Contains(got, "helper") {
			t.Fatalf("expected unexported symbols via -u:\n%s", got)
		}
		if strings.Contains(got, "return helper()") {
			t.Fatalf("focus package bodies must not appear in the initial context:\n%s", got)
		}
		if !strings.Contains(got, "TestExported") || !strings.Contains(got, "BenchmarkExported") {
			t.Fatalf("expected the test function names:\n%s", got)
		}
		if !strings.Contains(got, "focus note content") {
			t.Fatalf("expected the non-Go focus file at full content:\n%s", got)
		}
	})
}

func TestSimplifySingleFile(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
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
	// tokens / 4, rounded to the nearest multiple of
	// contextTokenBudgetUnit, with a floor at one unit. See
	// TheoryOfVisibilityAllocation.
	tests := []struct {
		focusTokens int
		want        int
	}{
		{0, contextTokenBudgetUnit},         // quarter=0 → rounds to 0 → floor at one unit
		{24 << 10, contextTokenBudgetUnit},  // quarter=6K → rounds to 0 → floor at one unit
		{128 << 10, contextTokenBudgetUnit}, // quarter=32K → exactly 32K
		{200 << 10, 64 << 10},               // quarter=50K → rounds to 64K
		{256 << 10, 64 << 10},               // quarter=64K → exactly 64K
		{400 << 10, 96 << 10},               // quarter=100K → rounds to 96K
	}
	for _, tt := range tests {
		got := calculateMaxContextTokens(tt.focusTokens)
		if got != tt.want {
			t.Errorf("for focus %d: expected %d, got %d", tt.focusTokens, tt.want, got)
		}
	}
}

func TestAllocateVisibilityUnaffordablePackageDoesNotBlockOthers(t *testing.T) {
	// An unaffordable higher-priority package must not blank out the
	// context for lower-priority packages. The old predecessor
	// constraint cascaded invisibility: if the first non-focus package in
	// priority order could not afford its minimum visibility (e.g., its go
	// doc output exceeded the budget, or go doc failed, making level 1's
	// sentinel cost 1<<30 unaffordable), every subsequent package was
	// capped at level 0 and the context used zero tokens of the 32K budget
	// even though cheaper packages would have fit. See
	// TheoryOfVisibilityAllocation.
	//
	// Context packages are excluded from this scenario: they are
	// explicitly requested via -ctx and are guaranteed their minimum
	// visibility (level 2) regardless of the budget. See
	// TestAllocateVisibilityGuaranteesContextPackages.
	t.Run("allocates lower priority when higher priority unaffordable", func(t *testing.T) {
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 0, 0, 100},
			},
			{
				PkgPath:       "directimport",
				Category:      CategoryDirectImport,
				MinVisibility: VisibilityDoc,
				Visibility:    VisibilityInvisible,
				// Level 1 (doc) costs 40000, exceeding the 32K budget
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

		if err := allocateVisibility(pkgs, logs.Logger{}, false, nil, nil); err != nil {
			t.Fatal(err)
		}

		if pkgs[0].Visibility != VisibilityDoc {
			t.Fatalf("focus should be pinned at level 1, got %d", pkgs[0].Visibility)
		}
		if pkgs[1].Visibility != VisibilityInvisible {
			t.Fatalf("directimport should be invisible (unaffordable), got %d", pkgs[1].Visibility)
		}
		if pkgs[2].Visibility == VisibilityInvisible {
			t.Fatalf("samemodule should be visible despite the unaffordable directimport package, got %d", pkgs[2].Visibility)
		}
	})

	t.Run("construction principle holds when all packages affordable", func(t *testing.T) {
		// The construction principle — higher-priority packages have at
		// least as much visibility as lower-priority ones — now holds
		// among non-focus packages. Focus packages are pinned at level 1
		// by design and may sit below context packages; the pinned focus
		// level is independent of the principle. See
		// TheoryOfVisibilityAllocation.
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
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

		if err := allocateVisibility(pkgs, logs.Logger{}, false, nil, nil); err != nil {
			t.Fatal(err)
		}

		if pkgs[0].Visibility != VisibilityDoc {
			t.Fatalf("focus should be pinned at level 1, got %d", pkgs[0].Visibility)
		}
		if pkgs[1].Visibility < pkgs[2].Visibility {
			t.Fatalf("construction principle violated: context (%d) < samemodule (%d)",
				pkgs[1].Visibility, pkgs[2].Visibility)
		}
	})

	t.Run("failed doc falls back to code visibility", func(t *testing.T) {
		// When go doc fails for a package, the doc is treated as empty
		// (zero cost, nothing emitted) rather than unaffordable, so the
		// water-fill can still upgrade the package to level 2 (code) using
		// the precomputed code costs. See TheoryOfLazyPackageDoc.
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 0, 0, 100},
			},
			{
				PkgPath:             "dep",
				Category:            CategoryDirectImport,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 1 << 30, 200, 300},
				TokensByLevel:       [4]int{0, 0, 200, 300},
			},
		}
		computeDoc := func(lp *LogicalPackage) {
			// Simulate a failed go doc: empty doc, zero cost.
			lp.DocContent = ""
			lp.DocTokens = 0
			lp.BudgetTokensByLevel[VisibilityDoc] = 0
			lp.TokensByLevel[VisibilityDoc] = 0
			lp.docComputed = true
		}
		if err := allocateVisibility(pkgs, logs.Logger{}, false, computeDoc, nil); err != nil {
			t.Fatal(err)
		}

		if pkgs[1].Visibility != VisibilityAll {
			t.Fatalf("dep should reach level 3 via the failed-doc fallback, got %d", pkgs[1].Visibility)
		}
	})
}

func TestAllocateVisibilityGuaranteesContextPackages(t *testing.T) {
	// A package explicitly requested via -ctx (CategoryContext) must be
	// included at its minimum visibility (level 2, code) even when its
	// code cost exceeds the context token budget. Without the guarantee,
	// the context package could not afford its code cost at minimum
	// allocation, so it stayed invisible or doc-only while its smaller
	// dependencies — discovered automatically — were water-filled to full
	// code: the explicitly requested package was absent from the context
	// while its dependencies were present. See
	// TheoryOfVisibilityAllocation.
	pkgs := []*LogicalPackage{
		{
			PkgPath:             "focus",
			Category:            CategoryFocus,
			MinVisibility:       VisibilityDoc,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [4]int{0, 0, 0, 100},
			TokensByLevel:       [4]int{0, 0, 0, 100},
		},
		{
			// The explicitly requested context package: its code (level 2)
			// costs 60000 tokens, exceeding the 32K context budget. It
			// must still be allocated.
			PkgPath:             "requested",
			Category:            CategoryContext,
			MinVisibility:       VisibilityCode,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [4]int{0, 2000, 60000, 60000},
			TokensByLevel:       [4]int{0, 2000, 60000, 60000},
		},
		{
			// A dependency of the requested package, discovered
			// automatically: small, so it would fit the budget easily.
			PkgPath:             "dependency",
			Category:            CategorySameModule,
			MinVisibility:       VisibilityDoc,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [4]int{0, 100, 500, 500},
			TokensByLevel:       [4]int{0, 100, 500, 500},
		},
	}

	if err := allocateVisibility(pkgs, logs.Logger{}, false, nil, nil); err != nil {
		t.Fatal(err)
	}

	if pkgs[0].Visibility != VisibilityDoc {
		t.Fatalf("focus should be pinned at level 1, got %d", pkgs[0].Visibility)
	}
	if pkgs[1].Visibility != VisibilityCode {
		t.Fatalf("context package must be guaranteed at level 2, got %d", pkgs[1].Visibility)
	}
	// The dependency must not be included: the context package exhausted
	// the budget. Before the guarantee, the dependency would have been
	// allocated instead, inverting the user's intent.
	if pkgs[2].Visibility != VisibilityInvisible {
		t.Fatalf("dependency should be invisible when the context package exhausts the budget, got %d", pkgs[2].Visibility)
	}
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
				// Focus is pinned at level 1, so its documentation is
				// probed too.
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
		if err := allocateVisibility(pkgs, logs.Logger{}, false, computeDoc, nil); err != nil {
			t.Fatal(err)
		}

		if len(docCalls) != 2 || docCalls[0] != "focus" || docCalls[1] != "samemodule" {
			t.Fatalf("expected doc computed once each for focus and samemodule, got %v", docCalls)
		}
		if pkgs[1].Visibility != VisibilityDoc {
			t.Fatalf("expected samemodule at level 1, got %d", pkgs[1].Visibility)
		}
	})

	t.Run("SkipsDocForPackagesBlockedByPredecessor", func(t *testing.T) {
		// The water-fill gates the 0→1 (doc) transition on the immediate
		// predecessor: a package whose predecessor is stuck at level 0 is
		// never probed, so the go doc subprocess does not run for packages
		// whose doc could not be shown without violating the priority
		// ordering. The docComputed guard additionally prevents repeated
		// probes on later water-fill iterations.
		// See TheoryOfLazyPackageDoc.
		//
		// Context packages are excluded from this scenario: they are
		// guaranteed their minimum visibility (level 2) and never sit at
		// level 0. See TestAllocateVisibilityGuaranteesContextPackages.
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
				// Direct-import package whose doc AND code costs exceed
				// the budget: it stays invisible and is probed exactly
				// once — the single unavoidable go doc invocation needed
				// to learn its real doc cost during the minimum
				// visibility allocation.
				PkgPath:             "directimport",
				Category:            CategoryDirectImport,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 60000, 60000, 60000},
			},
			{
				// An other-module package: its immediate predecessor is
				// also at level 0, so it is blocked at the 0→1 gate and
				// its doc is never probed.
				PkgPath:             "othermodule",
				Category:            CategoryOtherModule,
				MinVisibility:       VisibilityInvisible,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [4]int{0, 60000, 60000, 60000},
			},
			{
				// Same-module package: affordable at level 1, probed once,
				// and water-filled all the way to level 3.
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
		if err := allocateVisibility(pkgs, logs.Logger{}, false, computeDoc, nil); err != nil {
			t.Fatal(err)
		}

		// directimport is probed exactly once; the docComputed guard
		// prevents repeated go doc invocations on later water-fill
		// iterations.
		directImportProbes := 0
		for _, p := range docCalls {
			if p == "directimport" {
				directImportProbes++
			}
		}
		if directImportProbes != 1 {
			t.Fatalf("expected one doc probe for directimport, got %v", docCalls)
		}
		// othermodule never reaches level 1 and must never have its doc
		// computed.
		for _, p := range docCalls {
			if p == "othermodule" {
				t.Fatalf("doc must not be computed for a package blocked by its predecessor: %v", docCalls)
			}
		}
		// samemodule is probed once and reaches full visibility despite
		// the unaffordable packages ahead of it.
		if pkgs[3].Visibility != VisibilityAll {
			t.Fatalf("expected samemodule at level 3, got %d", pkgs[3].Visibility)
		}
	})
}

func TestAllocateVisibilityLazyCostComputation(t *testing.T) {
	// File token costs (rendered content and token counts at visibility
	// levels 2 and 3) are computed lazily, driven by the visibility
	// allocation: only packages the allocation actually probes run the
	// tokenizer. Context packages are always probed (their minimum
	// visibility is level 2); focus packages are pinned at documentation
	// level and never need file costs. A package that receives no
	// visibility — here, an other-module package whose doc and code costs
	// exceed the budget — must never have its costs computed. See
	// TheoryOfLazyVisibilityCosts.
	var costCalls []string
	computed := make(map[string]bool)
	computeCosts := func(lp *LogicalPackage) error {
		if computed[lp.PkgPath] {
			return nil
		}
		computed[lp.PkgPath] = true
		costCalls = append(costCalls, lp.PkgPath)
		return nil
	}

	pkgs := []*LogicalPackage{
		{
			PkgPath:             "focus",
			Category:            CategoryFocus,
			MinVisibility:       VisibilityAll,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [4]int{0, 0, 0, 100},
			TokensByLevel:       [4]int{0, 0, 0, 100},
		},
		{
			PkgPath:             "context",
			Category:            CategoryContext,
			MinVisibility:       VisibilityCode,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [4]int{0, 0, 100, 1 << 30},
			TokensByLevel:       [4]int{0, 0, 100, 1 << 30},
		},
		{
			PkgPath:             "othermodule",
			Category:            CategoryOtherModule,
			MinVisibility:       VisibilityInvisible,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [4]int{0, 1 << 30, 1 << 30, 1 << 30},
			TokensByLevel:       [4]int{0, 1 << 30, 1 << 30, 1 << 30},
		},
	}

	if err := allocateVisibility(pkgs, logs.Logger{}, false, nil, computeCosts); err != nil {
		t.Fatal(err)
	}

	if len(costCalls) != 1 || costCalls[0] != "context" {
		t.Fatalf("expected costs computed for context only (focus is pinned at documentation level), got %v", costCalls)
	}
	if pkgs[1].Visibility != VisibilityCode {
		t.Fatalf("expected context at level 2, got %d", pkgs[1].Visibility)
	}
	if pkgs[2].Visibility != VisibilityInvisible {
		t.Fatalf("expected othermodule invisible, got %d", pkgs[2].Visibility)
	}
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
	// documentation tokens, the dynamic context budget, and how the
	// context packages consume that budget by visibility level. See
	// TheoryOfTokenComposition.
	pkgs := []*LogicalPackage{
		{
			PkgPath:       "focus",
			Category:      CategoryFocus,
			Visibility:    VisibilityDoc,
			TokensByLevel: [4]int{0, 500, 0, 0},
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
