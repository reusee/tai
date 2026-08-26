package gotools

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

		// A large budget keeps the focus pin at full documentation: a
		// small maxTokens would trigger the overflow downgrade to short
		// doc and replace the focus documentation block this test
		// asserts on. See TheoryOfVisibilityAllocation.
		files, err = simplifyFiles(files, 1<<20, generators.DeepseekTokenCounterFn)
		if err != nil {
			t.Fatal(err)
		}
		// Focus packages are pinned at documentation level: the output is
		// the dep1 context file and the focus package's go doc block.
		// main.go and the non-Go focus file a.txt are never emitted as
		// files — every focus file is present by name in the focus
		// documentation block's file list. See TheoryOfNonGoFiles in
		// module_root.go.
		if len(files) < 2 {
			t.Fatalf("got %v", len(files))
		}
		t.Logf("num files: %v", len(files))
		var foundDep1, foundFocusDoc bool
		for _, f := range files {
			switch f.Path {
			case filepath.Join(dir, "..", "dep1", "dep1.go"):
				foundDep1 = true
				if !strings.Contains(f.Confirmed.What, "visibility level") {
					t.Fatalf("dep1.go should be at a code visibility level, got %q", f.Confirmed.What)
				}
			case filepath.Join(dir, "a.txt"):
				t.Fatalf("a.txt must not be emitted at full content; non-Go focus files are listed by name only")
			case filepath.Join(dir, "main.go"):
				t.Fatalf("main.go must not appear at full content; focus packages are documentation-only")
			}
			if strings.Contains(f.Confirmed.What, "focus go doc -u") {
				foundFocusDoc = true
				content := string(f.Confirmed.Content)
				if !strings.Contains(content, "begin of focus package") {
					t.Fatalf("focus documentation block missing its marker:\n%s", content)
				}
				if !strings.Contains(content, filepath.Join(dir, "a.txt")) {
					t.Fatalf("focus documentation block must list a.txt in its file list:\n%s", content)
				}
			}
		}
		if !foundDep1 {
			t.Fatal("dep1.go not found in output")
		}
		if !foundFocusDoc {
			t.Fatal("focus package documentation block not found in output")
		}

	})
}

func TestFocusPackageDocumentationContext(t *testing.T) {
	// Focus packages are pinned at documentation level: the initial
	// context carries go doc -all -cmd -u output (unexported symbols
	// included) plus the package's test-function names and file names,
	// and the model fetches implementation source on demand with go-src
	// blocks. Non-Go focus files are present by name only, in the file
	// list; their contents are fetched on demand with ingest blocks. See
	// TheoryOfVisibilityAllocation and TheoryOfNonGoFiles in
	// module_root.go.
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
		if !strings.Contains(got, "Files in this package") ||
			!strings.Contains(got, "focusdoc.go") ||
			!strings.Contains(got, "focusdoc_test.go") {
			t.Fatalf("expected the package file names in the focus block:\n%s", got)
		}
		if !strings.Contains(got, "notes.txt") {
			t.Fatalf("expected the non-Go focus file to be listed by name:\n%s", got)
		}
		if strings.Contains(got, "focus note content") {
			t.Fatalf("non-Go focus file contents must not appear in the initial context:\n%s", got)
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
		provider PartsProvider,
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
	// doc output exceeded the budget, or go doc failed, making the doc
	// level's sentinel cost 1<<30 unaffordable), every subsequent package
	// was capped at invisible and the context used zero tokens of the 32K
	// budget even though cheaper packages would have fit. See
	// TheoryOfVisibilityAllocation.
	//
	// Context packages are excluded from this scenario: they are
	// explicitly requested via -ctx and are guaranteed their minimum
	// visibility (code) regardless of the budget. See
	// TestAllocateVisibilityGuaranteesContextPackages.
	t.Run("allocates lower priority when higher priority unaffordable", func(t *testing.T) {
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 0, 0, 0, 100},
			},
			{
				PkgPath:       "directimport",
				Category:      CategoryDirectImport,
				MinVisibility: VisibilityDoc,
				Visibility:    VisibilityInvisible,
				// Both documentation levels (35000 short, 40000 full)
				// exceed the remaining budget
				BudgetTokensByLevel: [5]int{0, 35000, 40000, 50000, 50000},
			},
			{
				PkgPath:       "samemodule",
				Category:      CategorySameModule,
				MinVisibility: VisibilityDoc,
				Visibility:    VisibilityInvisible,
				// Both documentation levels cost little, affordable on
				// their own
				BudgetTokensByLevel: [5]int{0, 30, 50, 50, 50},
			},
		}

		if err := allocateVisibility(pkgs, logs.Logger{}, false, 0, nil, nil, nil); err != nil {
			t.Fatal(err)
		}

		if pkgs[0].Visibility != VisibilityDoc {
			t.Fatalf("focus should be pinned at full doc, got %d", pkgs[0].Visibility)
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
		// among non-focus packages. Focus packages are pinned at full
		// documentation by design and may sit below context packages; the
		// pinned focus level is independent of the principle. See
		// TheoryOfVisibilityAllocation.
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 0, 0, 0, 100},
			},
			{
				PkgPath:             "context",
				Category:            CategoryContext,
				MinVisibility:       VisibilityCode,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 50, 100, 200, 300},
			},
			{
				PkgPath:             "samemodule",
				Category:            CategorySameModule,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 25, 50, 100, 150},
			},
		}

		if err := allocateVisibility(pkgs, logs.Logger{}, false, 0, nil, nil, nil); err != nil {
			t.Fatal(err)
		}

		if pkgs[0].Visibility != VisibilityDoc {
			t.Fatalf("focus should be pinned at full doc, got %d", pkgs[0].Visibility)
		}
		if pkgs[1].Visibility < pkgs[2].Visibility {
			t.Fatalf("construction principle violated: context (%d) < samemodule (%d)",
				pkgs[1].Visibility, pkgs[2].Visibility)
		}
	})

	t.Run("failed doc falls back to code visibility", func(t *testing.T) {
		// When go doc fails for a package, the doc is treated as empty
		// (zero cost, nothing emitted) rather than unaffordable, so the
		// water-fill can still upgrade the package to code using the
		// precomputed code costs. See TheoryOfLazyPackageDoc.
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 0, 0, 0, 100},
			},
			{
				PkgPath:             "dep",
				Category:            CategoryDirectImport,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 1 << 30, 1 << 30, 200, 300},
				TokensByLevel:       [5]int{0, 0, 0, 200, 300},
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
		if err := allocateVisibility(pkgs, logs.Logger{}, false, 0, nil, computeDoc, nil); err != nil {
			t.Fatal(err)
		}

		if pkgs[1].Visibility != VisibilityAll {
			t.Fatalf("dep should reach full visibility via the failed-doc fallback, got %d", pkgs[1].Visibility)
		}
	})
}

func TestAllocateVisibilityGuaranteesContextPackages(t *testing.T) {
	// A package explicitly requested via -ctx (CategoryContext) must be
	// included at its minimum visibility (code, full Go code without test
	// files) even when its code cost exceeds the context token budget.
	// Without the guarantee, the context package could not afford its code
	// cost at minimum allocation, so it stayed invisible or doc-only while
	// its smaller dependencies — discovered automatically — were
	// water-filled to full code: the explicitly requested package was
	// absent from the context while its dependencies were present. See
	// TheoryOfVisibilityAllocation.
	pkgs := []*LogicalPackage{
		{
			PkgPath:             "focus",
			Category:            CategoryFocus,
			MinVisibility:       VisibilityDoc,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [5]int{0, 0, 0, 0, 100},
			TokensByLevel:       [5]int{0, 0, 0, 0, 100},
		},
		{
			// The explicitly requested context package: its code costs
			// 60000 tokens, exceeding the 32K context budget. It must
			// still be allocated.
			PkgPath:             "requested",
			Category:            CategoryContext,
			MinVisibility:       VisibilityCode,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [5]int{0, 1000, 2000, 60000, 60000},
			TokensByLevel:       [5]int{0, 1000, 2000, 60000, 60000},
		},
		{
			// A dependency of the requested package, discovered
			// automatically: small, so it would fit the budget easily.
			PkgPath:             "dependency",
			Category:            CategorySameModule,
			MinVisibility:       VisibilityDoc,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [5]int{0, 50, 100, 500, 500},
			TokensByLevel:       [5]int{0, 50, 100, 500, 500},
		},
	}

	if err := allocateVisibility(pkgs, logs.Logger{}, false, 0, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if pkgs[0].Visibility != VisibilityDoc {
		t.Fatalf("focus should be pinned at full doc, got %d", pkgs[0].Visibility)
	}
	if pkgs[1].Visibility != VisibilityCode {
		t.Fatalf("context package must be guaranteed at code level, got %d", pkgs[1].Visibility)
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
	// packages that actually reach a documentation level. The eager approach
	// ran go doc for every non-focus package in the dependency graph — one
	// Go toolchain subprocess per package — even though most packages end
	// invisible or at the code/full levels, where the doc output is never
	// used. See TheoryOfLazyPackageDoc.

	t.Run("ComputesDocForLevelOnePackages", func(t *testing.T) {
		var docCalls []string
		pkgs := []*LogicalPackage{
			{
				// Focus is pinned at full documentation, so its
				// documentation is probed too.
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 0, 0, 0, 100},
			},
			{
				// Same-module package whose doc fits the budget but whose
				// full code does not: it lands at full doc, so its doc is
				// computed exactly once.
				PkgPath:             "samemodule",
				Category:            CategorySameModule,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 50, 100, 1 << 30, 1 << 30},
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
		if err := allocateVisibility(pkgs, logs.Logger{}, false, 0, nil, computeDoc, nil); err != nil {
			t.Fatal(err)
		}

		if len(docCalls) != 2 || docCalls[0] != "focus" || docCalls[1] != "samemodule" {
			t.Fatalf("expected doc computed once each for focus and samemodule, got %v", docCalls)
		}
		if pkgs[1].Visibility != VisibilityDoc {
			t.Fatalf("expected samemodule at full doc, got %d", pkgs[1].Visibility)
		}
	})

	t.Run("SkipsDocForPackagesBlockedByPredecessor", func(t *testing.T) {
		// The water-fill gates both documentation transitions on the
		// immediate predecessor: a package whose predecessor is still at
		// a lower level is never probed, so the go doc subprocess does not
		// run for packages whose doc could not be shown without violating
		// the priority ordering. The docComputed guard additionally
		// prevents repeated probes on later water-fill iterations.
		// See TheoryOfLazyPackageDoc.
		//
		// Context packages are excluded from this scenario: they are
		// guaranteed their minimum visibility (code) and never sit at
		// invisible. See TestAllocateVisibilityGuaranteesContextPackages.
		var docCalls []string
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 0, 0, 0, 100},
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
				BudgetTokensByLevel: [5]int{0, 60000, 60000, 60000, 60000},
			},
			{
				// An other-module package: its immediate predecessor is
				// also at invisible, so it is blocked at the documentation
				// gate and its doc is never probed.
				PkgPath:             "othermodule",
				Category:            CategoryOtherModule,
				MinVisibility:       VisibilityInvisible,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 60000, 60000, 60000, 60000},
			},
			{
				// Same-module package: affordable at full doc, probed
				// once, and water-filled all the way to full content.
				PkgPath:             "samemodule",
				Category:            CategorySameModule,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 20, 50, 50, 50},
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
		if err := allocateVisibility(pkgs, logs.Logger{}, false, 0, nil, computeDoc, nil); err != nil {
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
		// othermodule never reaches full doc and must never have its doc
		// computed.
		for _, p := range docCalls {
			if p == "othermodule" {
				t.Fatalf("doc must not be computed for a package blocked by its predecessor: %v", docCalls)
			}
		}
		// samemodule is probed once and reaches full visibility despite
		// the unaffordable packages ahead of it.
		if pkgs[3].Visibility != VisibilityAll {
			t.Fatalf("expected samemodule at full visibility, got %d", pkgs[3].Visibility)
		}
	})
}

func TestAllocateVisibilityLazyCostComputation(t *testing.T) {
	// File token costs (rendered content and token counts at the code and
	// full visibility levels) are computed lazily, driven by the visibility
	// allocation: only packages the allocation actually probes run the
	// tokenizer. Context packages are always probed (their minimum
	// visibility is code); focus packages are pinned at full documentation
	// and never need file costs. A package that receives no visibility —
	// here, an other-module package whose doc and code costs exceed the
	// budget — must never have its costs computed. See
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
			MinVisibility:       VisibilityDoc,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [5]int{0, 0, 0, 0, 100},
			TokensByLevel:       [5]int{0, 0, 0, 0, 100},
		},
		{
			PkgPath:             "context",
			Category:            CategoryContext,
			MinVisibility:       VisibilityCode,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [5]int{0, 0, 0, 100, 1 << 30},
			TokensByLevel:       [5]int{0, 0, 0, 100, 1 << 30},
		},
		{
			PkgPath:             "othermodule",
			Category:            CategoryOtherModule,
			MinVisibility:       VisibilityInvisible,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [5]int{0, 1 << 30, 1 << 30, 1 << 30, 1 << 30},
			TokensByLevel:       [5]int{0, 1 << 30, 1 << 30, 1 << 30, 1 << 30},
		},
	}

	if err := allocateVisibility(pkgs, logs.Logger{}, false, 0, nil, nil, computeCosts); err != nil {
		t.Fatal(err)
	}

	if len(costCalls) != 1 || costCalls[0] != "context" {
		t.Fatalf("expected costs computed for context only (focus is pinned at full documentation), got %v", costCalls)
	}
	if pkgs[1].Visibility != VisibilityCode {
		t.Fatalf("expected context at code level, got %d", pkgs[1].Visibility)
	}
	if pkgs[2].Visibility != VisibilityInvisible {
		t.Fatalf("expected othermodule invisible, got %d", pkgs[2].Visibility)
	}
}

func TestAllocateVisibilityShortDoc(t *testing.T) {
	// Short doc is the water-fill's first documentation step from
	// invisible: a package whose full doc exceeds the budget but whose
	// short doc fits lands at VisibilityShortDoc, so a tight budget
	// yields a briefly-documented package instead of an invisible one.
	// No category has short doc as its minimum visibility, so the
	// minimum-visibility allocation never probes short doc; the probe
	// happens only in the water-fill, computed exactly once per package.
	// Focus packages are pinned at full documentation and never occupy
	// the short-doc level. See TheoryOfVisibilityAllocation and
	// TheoryOfLazyPackageDoc.
	var shortDocCalls []string
	computeShortDoc := func(lp *LogicalPackage) {
		if lp.shortDocComputed {
			return
		}
		shortDocCalls = append(shortDocCalls, lp.PkgPath)
		lp.ShortDocContent = "short doc"
		lp.ShortDocTokens = 100
		lp.BudgetTokensByLevel[VisibilityShortDoc] = 100
		lp.TokensByLevel[VisibilityShortDoc] = 100
		lp.shortDocComputed = true
	}

	pkgs := []*LogicalPackage{
		{
			PkgPath:             "focus",
			Category:            CategoryFocus,
			MinVisibility:       VisibilityDoc,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [5]int{0, 0, 0, 0, 100},
			TokensByLevel:       [5]int{0, 0, 0, 0, 100},
		},
		{
			// A direct import whose full doc (40000) exceeds the 32K
			// budget: the minimum-visibility allocation leaves it
			// invisible, and the water-fill upgrades it to short doc,
			// the affordable documentation level.
			PkgPath:             "directimport",
			Category:            CategoryDirectImport,
			MinVisibility:       VisibilityDoc,
			Visibility:          VisibilityInvisible,
			BudgetTokensByLevel: [5]int{0, 100, 40000, 50000, 50000},
			TokensByLevel:       [5]int{0, 100, 40000, 50000, 50000},
		},
	}

	if err := allocateVisibility(pkgs, logs.Logger{}, false, 0, computeShortDoc, nil, nil); err != nil {
		t.Fatal(err)
	}

	if pkgs[1].Visibility != VisibilityShortDoc {
		t.Fatalf("expected directimport at short doc, got %d", pkgs[1].Visibility)
	}
	if len(shortDocCalls) != 1 || shortDocCalls[0] != "directimport" {
		t.Fatalf("expected short doc computed exactly once for directimport, got %v", shortDocCalls)
	}
}

func TestPrefetchFocusShortDocs(t *testing.T) {
	// The focus overflow downgrade computes every focus package's short
	// doc unconditionally. When the downgrade condition already holds —
	// the pinned focus tokens exceed the generator budget —
	// prefetchFocusShortDocs precomputes the short docs concurrently so
	// the downgrade loop in allocateVisibility short-circuits via the
	// shortDocComputed guard; when the condition does not hold, short
	// doc stays fully lazy. See TheoryOfLazyPackageDoc and
	// TheoryOfVisibilityAllocation in visibility.go.
	countTokens := func(s string) (int, error) { return len(s), nil }

	newPkgs := func() []*LogicalPackage {
		return []*LogicalPackage{
			{
				PkgPath:       "focusa",
				Category:      CategoryFocus,
				MinVisibility: VisibilityDoc,
				TokensByLevel: [5]int{0, 0, 300, 0, 0},
			},
			{
				PkgPath:       "focusb",
				Category:      CategoryFocus,
				MinVisibility: VisibilityDoc,
				TokensByLevel: [5]int{0, 0, 300, 0, 0},
			},
			{
				// The -all-src pin: the pinned level is full source, so
				// the overflow sum reads TokensByLevel[VisibilityAll].
				PkgPath:       "focusall",
				Category:      CategoryFocus,
				MinVisibility: VisibilityAll,
				TokensByLevel: [5]int{0, 0, 0, 0, 400},
			},
			{
				PkgPath:       "other",
				Category:      CategoryOtherModule,
				MinVisibility: VisibilityInvisible,
			},
		}
	}

	// Pinned focus tokens (1000) exactly match the budget: the downgrade
	// condition is strict inequality, so no short-doc probe is launched
	// and every package stays un-computed.
	pkgs := newPkgs()
	prefetchFocusShortDocs(pkgs, t.TempDir(), nil, countTokens, 1000)
	for _, lp := range pkgs {
		if lp.shortDocComputed {
			t.Fatalf("expected %s short doc to stay lazy", lp.PkgPath)
		}
	}

	// Pinned focus tokens (1000) exceed the budget (999): the downgrade
	// is certain, so every focus package's short doc is computed — in the
	// module-less temp dir the subprocess fails, which still records
	// shortDocComputed and non-empty short-doc content — while the
	// non-focus package stays untouched.
	pkgs = newPkgs()
	prefetchFocusShortDocs(pkgs, t.TempDir(), nil, countTokens, 999)
	for _, lp := range pkgs {
		if lp.Category != CategoryFocus {
			if lp.shortDocComputed {
				t.Fatalf("expected non-focus %s short doc untouched", lp.PkgPath)
			}
			continue
		}
		if !lp.shortDocComputed {
			t.Fatalf("expected focus %s short doc computed", lp.PkgPath)
		}
		if lp.ShortDocContent == "" || lp.TokensByLevel[VisibilityShortDoc] == 0 {
			t.Fatalf("expected focus %s short-doc content recorded", lp.PkgPath)
		}
	}

	// A zero budget disables the downgrade check in allocateVisibility,
	// so the prefetch must not fire either.
	pkgs = newPkgs()
	prefetchFocusShortDocs(pkgs, t.TempDir(), nil, countTokens, 0)
	for _, lp := range pkgs {
		if lp.shortDocComputed {
			t.Fatalf("expected %s short doc to stay lazy with maxTokens 0", lp.PkgPath)
		}
	}
}

func TestAllocateVisibilityDowngradesFocusToShortDoc(t *testing.T) {
	// When the pinned focus tokens exceed the generator token budget, the
	// focus block alone overflows the model's window: allocateVisibility
	// downgrades focus packages to the short-doc level and re-derives the
	// context budget from the downgraded tokens. The re-derivation is
	// observable through the context allocation: a package whose full doc
	// is affordable only under the full-doc-derived budget (65536 from
	// 200<<10 focus tokens) becomes unaffordable under the
	// short-doc-derived floor (32768). A maxTokens of 0 disables the
	// check. See TheoryOfVisibilityAllocation.
	t.Run("downgrades and re-derives the budget", func(t *testing.T) {
		var docCalls, shortDocCalls []string
		computeDoc := func(lp *LogicalPackage) {
			if lp.docComputed {
				return
			}
			docCalls = append(docCalls, lp.PkgPath)
			lp.DocContent = "doc"
			lp.DocTokens = 200 << 10
			lp.BudgetTokensByLevel[VisibilityDoc] = 200 << 10
			lp.TokensByLevel[VisibilityDoc] = 200 << 10
			lp.docComputed = true
		}
		computeShortDoc := func(lp *LogicalPackage) {
			if lp.shortDocComputed {
				return
			}
			shortDocCalls = append(shortDocCalls, lp.PkgPath)
			lp.ShortDocContent = "short doc"
			lp.ShortDocTokens = 1 << 10
			lp.BudgetTokensByLevel[VisibilityShortDoc] = 1 << 10
			lp.TokensByLevel[VisibilityShortDoc] = 1 << 10
			lp.shortDocComputed = true
		}
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 1 << 30, 1 << 30, 1 << 30, 1 << 30},
			},
			{
				// Full doc costs 40000: affordable when the budget derives
				// from the full focus doc (65536) but not from the
				// downgraded short doc (32768 floor), proving the budget
				// was re-derived after the downgrade.
				PkgPath:             "samemodule",
				Category:            CategorySameModule,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 1 << 30, 40000, 40000, 40000},
			},
		}

		// The pinned focus doc (200<<10) exceeds the generator budget
		// (150<<10), so the downgrade fires.
		if err := allocateVisibility(pkgs, logs.Logger{}, false, 150<<10, computeShortDoc, computeDoc, nil); err != nil {
			t.Fatal(err)
		}

		if pkgs[0].Visibility != VisibilityShortDoc {
			t.Fatalf("focus should be downgraded to short doc, got %d", pkgs[0].Visibility)
		}
		if len(shortDocCalls) == 0 || shortDocCalls[0] != "focus" {
			t.Fatalf("expected short doc computed for the focus downgrade first, got %v", shortDocCalls)
		}
		if len(docCalls) == 0 || docCalls[0] != "focus" {
			t.Fatalf("expected full doc computed for the focus pin first, got %v", docCalls)
		}
		// Under the pre-downgrade budget (65536) samemodule would have
		// been allocated at full doc; under the re-derived floor (32768)
		// it cannot afford full doc and lands at short doc.
		if pkgs[1].Visibility != VisibilityShortDoc {
			t.Fatalf("samemodule should be at short doc under the re-derived budget, got %d", pkgs[1].Visibility)
		}
	})

	t.Run("keeps full doc when focus fits", func(t *testing.T) {
		// The fake doc costs are keyed by package: the focus doc drives
		// the overflow check, while samemodule's smaller doc cost drives
		// its own affordability.
		computeDoc := func(lp *LogicalPackage) {
			if lp.docComputed {
				return
			}
			tokens := 200 << 10
			if lp.PkgPath != "focus" {
				tokens = 40000
			}
			lp.DocContent = "doc"
			lp.DocTokens = tokens
			lp.BudgetTokensByLevel[VisibilityDoc] = tokens
			lp.TokensByLevel[VisibilityDoc] = tokens
			lp.docComputed = true
		}
		pkgs := []*LogicalPackage{
			{
				PkgPath:             "focus",
				Category:            CategoryFocus,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 1 << 30, 1 << 30, 1 << 30, 1 << 30},
			},
			{
				PkgPath:             "samemodule",
				Category:            CategorySameModule,
				MinVisibility:       VisibilityDoc,
				Visibility:          VisibilityInvisible,
				BudgetTokensByLevel: [5]int{0, 1 << 30, 40000, 1 << 30, 1 << 30},
			},
		}

		// A budget larger than the 200<<10 focus doc tokens: no
		// downgrade, focus pins at full doc, and the 65536 budget derived
		// from it affords samemodule's 40000-token doc.
		if err := allocateVisibility(pkgs, logs.Logger{}, false, 1<<20, nil, computeDoc, nil); err != nil {
			t.Fatal(err)
		}

		if pkgs[0].Visibility != VisibilityDoc {
			t.Fatalf("focus should stay pinned at full doc, got %d", pkgs[0].Visibility)
		}
		if pkgs[1].Visibility != VisibilityDoc {
			t.Fatalf("samemodule should be at full doc under the full-doc-derived budget, got %d", pkgs[1].Visibility)
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
	// documentation tokens, the dynamic context budget, and how the
	// context packages consume that budget by visibility level, including
	// the short-doc level. See TheoryOfTokenComposition.
	pkgs := []*LogicalPackage{
		{
			PkgPath:       "focus",
			Category:      CategoryFocus,
			Visibility:    VisibilityDoc,
			TokensByLevel: [5]int{0, 0, 500, 0, 0},
		},
		{
			PkgPath:       "shortdocpkg",
			Category:      CategoryOtherModule,
			Visibility:    VisibilityShortDoc,
			TokensByLevel: [5]int{0, 40, 90, 200, 300},
		},
		{
			PkgPath:       "docpkg",
			Category:      CategorySameModule,
			Visibility:    VisibilityDoc,
			TokensByLevel: [5]int{0, 50, 100, 300, 400},
		},
		{
			PkgPath:       "codepkg",
			Category:      CategoryContext,
			Visibility:    VisibilityCode,
			TokensByLevel: [5]int{0, 20, 50, 200, 300},
		},
		{
			PkgPath:       "fullpkg",
			Category:      CategorySameModule,
			Visibility:    VisibilityAll,
			TokensByLevel: [5]int{0, 5, 10, 20, 600},
		},
		{
			PkgPath:       "hiddenpkg",
			Category:      CategoryStdLib,
			Visibility:    VisibilityInvisible,
			TokensByLevel: [5]int{0, 15, 30, 40, 50},
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
		`"context tokens"=940`,
		`"short doc packages"=1`,
		`"doc packages"=1`,
		`"code packages"=1`,
		`"full packages"=1`,
		`"invisible packages"=1`,
		`"short doc tokens"=40`,
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
		provider PartsProvider,
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
