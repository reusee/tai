package gotools

import (
	"bytes"
	"fmt"
	"go/ast"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/pathutil"
)

const TheoryOfLazyPackageDoc = `
Package documentation (go doc output) is computed lazily, only for packages
that actually reach a documentation level. The eager approach ran go doc -all
-cmd for every non-focus package in the dependency graph during
precomputeTokenCounts, spawning one Go toolchain subprocess per package —
hundreds of processes for a typical project — even though most packages end
invisible or at the code/full levels, where the doc output is never used.
There are two documentation levels — short doc (go doc without -all) and
full doc (go doc -all -cmd) — and each is produced by its own subprocess
invocation, cached independently on the package (shortDocComputed,
ShortDocContent; docComputed, DocContent). The allocation algorithm only
requires a package's doc cost when it considers placing the package at that
level: in the minimum-visibility allocation (whose minimum is full doc),
when pinning focus packages at their documentation level, and in the
water-filling upgrades into a documentation level. At exactly those decision
points allocateVisibility invokes computeShortDoc or computeDoc, which runs
go doc once, caches the result on the package, and sets that level's token
costs. Until a package's doc at a level is computed, that level's budget
cost carries the sentinel 1<<30, which makes the level unaffordable; the
sentinel is never read by a decision because the computation runs first
whenever the level is considered.

The minimum-visibility allocation probes every package whose minimum
visibility includes full documentation (focus, same-module, and
direct-import packages): the doc cost must be known before affordability
can be decided. These unconditional probes are launched concurrently by
prefetchPackageDocs with bounded concurrency, hiding the per-subprocess
latency that a serial probe loop would incur; the allocation's own
computeDoc calls then short-circuit via the docComputed guard. Short doc is
never prefetched: no category has short doc as its minimum visibility, so
it is probed only by the water-fill.

The water-fill phase gates both documentation upgrades (invisible→short
doc and short doc→full doc) on the immediate predecessor: a package whose
predecessor is still at a lower level is not probed, because its doc could
not be shown without violating the priority ordering. This keeps the
number of go doc subprocesses proportional to the number of packages that
can actually benefit from documentation, rather than the size of the
dependency graph. When the minimum-visibility allocation exhausts the
budget — which happens when a guaranteed context package costs more than
the context budget — the water-fill phase is skipped entirely: no upgrade
is affordable, and probing the documentation transitions would only spawn
go doc subprocesses whose output could not be shown.

When go doc fails for a package, the doc is treated as empty rather than
unaffordable: the level's cost is zero and its content is empty (nothing
is emitted at that level), and the water-fill can still upgrade the
package to code, whose costs are precomputed. This turns a systemic go
doc failure into a graceful degradation to code visibility instead of
making every such package permanently invisible. A focus package is the
exception: it is pinned at full documentation and never upgraded, so
its block is still emitted with a failure note and the test-function
names, keeping the package discoverable for go-src fetches.
`

const TheoryOfContextStrategy = `
Code context construction sits between two poles. Full-source context
delivers every file upfront: no detail is missed, but tokens are
consumed at scale and the model's attention is diluted across code the
task never touches. Agentic exploration delivers no code upfront and
lets the model find it via semantic search: cheap in tokens, but prone
to missing details and to never grasping the architecture as a whole —
what the model does not search for never surfaces.

The project evolved from full source to a middle path that keeps the
strengths of both poles. The initial context is documentation: focus
packages enter as go doc output carrying the complete declaration
surface — every symbol, every test function name — at a fraction of
the full-source token cost, and the supporting package graph fills a
deterministic budget (TheoryOfVisibilityAllocation). Implementation
source is fetched on demand with go-src blocks, resolved against the
declaration surface the model already sees, so a fetch is a targeted
pull from a known index rather than a search in the dark. No detail is
unreachable, because the full surface precedes every fetch; no token is
spent on bodies the task never reads. Start from the whole picture, and
descend into detail on demand. TheoryOfGoSrcResolution describes the
fetch mechanism.
`

const TheoryOfVisibilityAllocation = `
The context token budget for non-focus packages is dynamic: it is derived
from the total token count of the focus packages at their pinned level —
documentation by default, full source under -all-src — so that large
focus surfaces receive a proportionally larger context budget. The budget
is focusTokens / 4, rounded to the nearest multiple of
contextTokenBudgetUnit (32K), with a floor at one unit. A repository
whose focus packages total 128K documentation tokens gets a 32K context
budget; a 200K documentation surface gets a 64K budget. This scaling
prevents a large focus package from starving its dependencies and
supporting packages, which would degrade the model's ability to reason
about cross-package interactions. Small projects stay at the 32K floor,
matching the original fixed-budget behavior. The computation is
deterministic: identical focus packages always produce an identical
budget, so context files are simplified to the same level across
requests with the same focus, preserving the LLM prefix cache.

The visibility allocation uses a water-filling algorithm that upgrades
packages from their minimum visibility to higher levels as the budget
allows. Packages are processed in priority order (highest first). Each
step upgrades the leftmost (highest priority) affordable package by one
level. A package that cannot afford a level is skipped rather than
blocking lower-priority packages, so a single unaffordable package
cannot blank out the entire context: the budget is shared, and every
package gets an independent chance to reach its minimum visibility.

Short doc is the cheapest documentation level: go doc without -all, the
package overview and top-level symbol index without per-symbol
documentation. No category has short doc as its minimum visibility; it
exists as the water-fill's first upgrade step from invisible, so a tight
budget yields many briefly-documented packages instead of a few
fully-documented ones.

The minimum-visibility allocation processes packages in priority order
and gives each package its minimum visibility if it fits in the
remaining budget; unaffordable packages are left invisible and do not
affect subsequent packages. Priority still governs the order of
allocation, so high-priority packages are allocated before the budget
is exhausted.

Context packages — those explicitly requested via -ctx or -dep — are
guaranteed their minimum visibility (VisibilityCode, full code without
test files): the minimum allocation deducts their cost from the budget
unconditionally, even when it exceeds the remaining budget. An
explicitly requested package must never be dropped in favor of its own
dependencies, which are discovered automatically. Without the guarantee,
a large context package could not afford its code cost at minimum
allocation and would be out-prioritized by its smaller dependencies
during water-fill, leaving the requested package invisible or doc-only
while its dependencies show full code. When a guaranteed context
package exhausts the budget, the water-fill phase is skipped entirely:
no upgrade is affordable, and probing packages would only spawn go doc
subprocesses whose results could not be shown.

The water-fill upgrade phase retains one predecessor gate: the upgrades
into the documentation levels — from invisible to short doc and from
short doc to full doc — are blocked when the immediately preceding
package is at a lower level, because those transitions require expensive
go doc subprocess probes (see TheoryOfLazyPackageDoc). Upgrades to code
and full content use precomputed costs and are never gated, so packages
behind an unaffordable one can still be upgraded all the way to full
code. This means the construction principle — higher-priority packages
have at least as much visibility as lower-priority ones — holds when
every package is affordable at its minimum, and degrades gracefully
otherwise: visibility may be non-monotonic only in the presence of
unaffordable packages.

Focus packages are pinned at full documentation: their initial context is
the declaration surface — go doc -all -cmd -u output plus the package's
test-function names — and the model fetches implementation source on
demand with go-src blocks. The -all-src flag switches the pin to full
source (VisibilityAll): every focus file including tests enters the
initial context, the focus documentation block is not produced, and
go-src fetching is unnecessary for focus declarations. Pinning bounds
the initial context of large projects without sacrificing detail: the
model pulls exactly the declarations it needs. The water-fill never
upgrades pinned focus packages (upgrading would reintroduce the
full-source initial context), focus never counts against the context
budget, and the budget derives from the focus tokens at the pinned
level, so the same focus packages always produce the same budget. Focus
files still reach the context at full content when explicitly requested
via -file (DoNotSimplify) and when they are non-Go files, which go doc
cannot summarize; the focus documentation block carries the writable-dir
read-only annotation in its begin marker, because focus Go files are no
longer emitted individually. The construction principle therefore holds
among non-focus packages; the pinned focus level is independent of it by
design.
`

const TheoryOfLazyVisibilityCosts = `
File token costs (rendered content and token counts at the code and full
visibility levels) are computed lazily, driven by the visibility
allocation. Only packages whose costs the allocation requires up front are
precomputed in parallel: context packages (their code-level cost is the
first minimum-visibility decision) and any package containing files always
emitted at full content — DoNotSimplify files and the non-Go files of
focus packages, which go doc cannot summarize. Focus packages need no
file costs at all: pinned at full documentation, their budget
contribution is their documentation size, so focus source files are
never rendered or token-counted unless a file is explicitly requested
via -file. Every other package computes its costs on demand, exactly
when the allocation probes it for a level that needs the file costs.
Packages that receive no visibility — typically the long tail of
external dependencies and standard library packages — never run the
tokenizer, and packages that stop at a documentation level never pay
the cost of counting their source files. The eagerly computed and
on-demand values are identical because token counting is deterministic,
so the allocation outcome is unchanged; laziness only avoids the work.

The water-fill's probe sequence bounds the on-demand computes
automatically: a package whose predecessor is stuck at invisible is
never probed for documentation, and file costs are only probed for
packages that actually crossed the documentation levels. In a typical
session with a 32K-96K context budget and a large dependency graph, only
the project's own packages and a few high-priority dependencies are ever
token-counted.
`

const TheoryOfRenderedFileCache = `
The rendered content of a file (including the begin/end markers) does not
depend on the visibility level; the code and full levels differ only in
which files are included. The full level includes every file, and the code
level includes only non-test Go files, so the full level's file set is a
superset of the code level's, and a file's render is identical at both
levels. The documentation levels render no files: their content is
per-package go doc output. computePackageCosts therefore renders and
token-counts each file exactly once and reuses the result across levels,
eliminating duplicate disk reads and tokenizer work per package. The
computation itself is lazy per package: only packages probed by the
visibility allocation — or context packages and packages with
always-full-content files, whose costs are required up front — are
rendered and counted at all. See TheoryOfLazyVisibilityCosts.
`

const TheoryOfGoDocReadonly = `
go doc runs with -mod=readonly (the go command's default) rather than the
-mod=mod injected into the load environment for go list. The -mod=mod flag
allows go list to update go.mod and go.sum when they are out of sync, but
go doc must not modify go.sum: go mod tidy removes checksums for modules
that are no longer needed, and go doc would add them back, causing go.sum
to churn between tidy and doc invocations. With -mod=readonly, go doc
fails instead of modifying go.sum when a checksum is missing; the failure
is handled gracefully by the visibility system (fallback to code
visibility) and surfaces as an error for -doc packages.
`

// contextTokenBudgetUnit is the base unit of the dynamic context token
// budget: it is both the floor (minimum budget) and the alignment
// multiple. The budget for context (non-focus) packages is
// focusTokens / 4 rounded to the nearest multiple of this unit, with a
// floor at one unit. See TheoryOfVisibilityAllocation.
const contextTokenBudgetUnit = 32 << 10

// calculateMaxContextTokens computes the dynamic token budget for context
// (non-focus) files from the total token count of the focus packages: the
// budget is focusTokens / 4, rounded to the nearest multiple of
// contextTokenBudgetUnit, with a floor at one unit. The computation is
// deterministic — identical focus packages always produce an identical
// budget — so context files are simplified to the same level across
// requests with the same focus, preserving the LLM prefix cache. Large
// repositories with big focus packages receive proportionally larger
// budgets so that context packages are not starved. See
// TheoryOfVisibilityAllocation.
func calculateMaxContextTokens(focusTokens int) int {
	quarter := focusTokens / 4
	rounded := ((quarter + contextTokenBudgetUnit/2) / contextTokenBudgetUnit) * contextTokenBudgetUnit
	if rounded < contextTokenBudgetUnit {
		rounded = contextTokenBudgetUnit
	}
	return rounded
}

// shouldIncludeFile reports whether a file should be included at the
// given visibility level. The documentation levels render no files —
// their content is per-package go doc output — and the code and full
// levels differ only in whether test files, non-Go files, and embed
// files are included.
func shouldIncludeFile(f *File, level VisibilityLevel) bool {
	switch level {
	case VisibilityInvisible, VisibilityShortDoc, VisibilityDoc:
		return false
	case VisibilityCode:
		return f.IsGoFile && !f.IsTestFile
	case VisibilityAll:
		return true
	}
	return false
}

// goDocOutput runs `go doc -all -cmd` for the package — adding -u for a
// focus package so unexported symbols are included — and returns the raw
// documentation text with a guaranteed trailing newline. Every
// documentation path (visibility-level rendering, focus-package pinning,
// and go-src package references) delegates here, keeping one go doc
// invocation shared across the codebase. go doc runs with -mod=readonly:
// the load environment injects -mod=mod (see TheoryOfModModEnv) so go
// list can update go.mod when it is out of sync, but go doc would then
// re-add checksums that go mod tidy removed, causing go.sum to churn.
// Stripping -mod=mod leaves the go command's default -mod=readonly,
// which fails instead of writing when a checksum is missing. See
// TheoryOfGoDocReadonly.
func goDocOutput(pkgPath, dir string, envs []string, focus bool) (string, error) {
	args := []string{"doc", "-all", "-cmd"}
	if focus {
		args = append(args, "-u")
	}
	args = append(args, pkgPath)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = withoutModModEnv(envs)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	text := string(output)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text, nil
}

// goDocShortOutput runs `go doc -cmd` without -all for the package and
// returns the raw text with a guaranteed trailing newline. The output is
// the package overview and the top-level symbol index — a fraction of the
// full documentation's size — used by the short-doc visibility level. The
// invocation shares the read-only module environment with goDocOutput. See
// TheoryOfGoDocReadonly and TheoryOfLazyPackageDoc.
func goDocShortOutput(pkgPath, dir string, envs []string) (string, error) {
	args := []string{"doc", "-cmd", pkgPath}
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = withoutModModEnv(envs)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	text := string(output)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text, nil
}

// renderPackageDoc runs `go doc -all -cmd` for the package and wraps
// the output with context package markers. Full documentation is
// per-package, not per-file. If go doc fails, computePackageDoc treats
// the doc as empty: VisibilityDoc costs nothing and emits nothing.
// The -u flag is deliberately omitted: including unexported symbols
// roughly doubles the doc output for most packages without adding
// API-level reference value. Focus packages use -u via
// computeFocusPackageDoc. See goDocOutput for the invocation.
func renderPackageDoc(
	pkgPath string,
	dir string,
	envs []string,
	countTokens func(string) (int, error),
) (content string, tokens int, err error) {
	text, err := goDocOutput(pkgPath, dir, envs, false)
	if err != nil {
		return "", 0, err
	}

	content = "``` begin of context package " + pkgPath + "\n" +
		text +
		"``` end of context package " + pkgPath + "\n"

	tokens, err = countTokens(content)
	if err != nil {
		return "", 0, err
	}
	return content, tokens, nil
}

// renderShortDoc runs `go doc` without -all for the package and wraps
// the output with context package markers. Short doc is per-package: the
// package overview and the top-level symbol index without per-symbol
// documentation, so it costs a fraction of the full documentation and
// serves as the water-fill's first documentation step when the budget is
// tight. If go doc fails, computePackageShortDoc treats the short doc as
// empty. See goDocShortOutput for the invocation.
func renderShortDoc(
	pkgPath string,
	dir string,
	envs []string,
	countTokens func(string) (int, error),
) (content string, tokens int, err error) {
	text, err := goDocShortOutput(pkgPath, dir, envs)
	if err != nil {
		return "", 0, err
	}

	content = "``` begin of context package " + pkgPath + "\n" +
		text +
		"``` end of context package " + pkgPath + "\n"

	tokens, err = countTokens(content)
	if err != nil {
		return "", 0, err
	}
	return content, tokens, nil
}

// computePackageDoc computes and caches the full-doc output for a logical
// package. It is the production computeDoc used by allocateVisibility via
// SimplifyFiles. Focus packages are routed to computeFocusPackageDoc:
// their documentation block carries -u output and test-function names and
// is the package's pinned terminal visibility, so it is never empty. For
// non-focus packages, on success the VisibilityDoc budget cost is the doc
// token count; on failure the doc is treated as empty: VisibilityDoc
// costs nothing and emits nothing, and the water-fill can still upgrade
// the package to code, whose costs are precomputed. This prevents a single
// go doc failure from making the package permanently unaffordable. The
// docComputed guard makes the call idempotent: each package runs the
// go doc subprocess at most once. See TheoryOfLazyPackageDoc.
func computePackageDoc(
	lp *LogicalPackage,
	dir string,
	envs Envs,
	countTokens func(string) (int, error),
) {
	if lp.docComputed {
		return
	}
	if lp.Category == CategoryFocus {
		computeFocusPackageDoc(lp, dir, envs, countTokens)
		return
	}
	content, tokens, err := renderPackageDoc(lp.PkgPath, dir, []string(envs), countTokens)
	if err != nil {
		// Treat a failed go doc as an empty doc: zero cost and zero
		// content, so the package emits nothing at full doc but can be
		// water-filled to code.
		// See TheoryOfLazyPackageDoc.
		lp.DocContent = ""
		lp.DocTokens = 0
		lp.BudgetTokensByLevel[VisibilityDoc] = 0
		lp.TokensByLevel[VisibilityDoc] = 0
	} else {
		lp.DocContent = content
		lp.DocTokens = tokens
		lp.BudgetTokensByLevel[VisibilityDoc] = tokens
		lp.TokensByLevel[VisibilityDoc] = tokens
	}
	lp.docComputed = true
}

// computePackageShortDoc computes and caches the short-doc output for a
// logical package: go doc without -all, wrapped with context package
// markers. It is the production computeShortDoc used by allocateVisibility
// via SimplifyFiles, invoked only for non-focus packages: focus packages
// are pinned at full documentation and never occupy the short-doc level.
// On success the short-doc budget cost is the token count; on failure the
// short doc is treated as empty: VisibilityShortDoc costs nothing and
// emits nothing, and the water-fill can still upgrade the package to full
// doc or code. The shortDocComputed guard makes the call idempotent: each
// package runs the short-doc subprocess at most once, independently of
// the full-doc computation. See TheoryOfLazyPackageDoc.
func computePackageShortDoc(
	lp *LogicalPackage,
	dir string,
	envs Envs,
	countTokens func(string) (int, error),
) {
	if lp.shortDocComputed {
		return
	}
	content, tokens, err := renderShortDoc(lp.PkgPath, dir, []string(envs), countTokens)
	if err != nil {
		lp.ShortDocContent = ""
		lp.ShortDocTokens = 0
		lp.BudgetTokensByLevel[VisibilityShortDoc] = 0
		lp.TokensByLevel[VisibilityShortDoc] = 0
	} else {
		lp.ShortDocContent = content
		lp.ShortDocTokens = tokens
		lp.BudgetTokensByLevel[VisibilityShortDoc] = tokens
		lp.TokensByLevel[VisibilityShortDoc] = tokens
	}
	lp.shortDocComputed = true
}

// computeFocusPackageDoc computes the full-doc block for a focus package:
// go doc -all -cmd -u output (unexported symbols included, because the
// model edits focus packages) followed by the package's test function
// names, wrapped in "focus package" markers so the model can distinguish
// the focus declaration surface from context documentation. The block is
// the focus package's pinned terminal visibility, so it is emitted even
// when go doc fails — carrying a failure note and the test names —
// keeping the package discoverable for go-src fetches. A countTokens
// failure falls back to empty content, matching the non-focus path. See
// TheoryOfVisibilityAllocation and TheoryOfLazyPackageDoc.
func computeFocusPackageDoc(
	lp *LogicalPackage,
	dir string,
	envs Envs,
	countTokens func(string) (int, error),
) {
	readOnlyNote := ""
	if focusPackageReadOnly(lp) {
		readOnlyNote = " (read-only)"
	}

	var body strings.Builder
	if text, err := goDocOutput(lp.PkgPath, dir, []string(envs), true); err != nil {
		body.WriteString("(go doc failed: " + err.Error() +
			"; fetch declarations with go-src blocks)\n")
	} else {
		body.WriteString(text)
	}
	body.WriteString(focusTestNamesSection(lp))

	content := "``` begin of focus package " + lp.PkgPath + readOnlyNote + "\n" +
		body.String() +
		"``` end of focus package " + lp.PkgPath + "\n"

	tokens, err := countTokens(content)
	if err != nil {
		content = ""
		tokens = 0
	}
	lp.DocContent = content
	lp.DocTokens = tokens
	lp.BudgetTokensByLevel[VisibilityDoc] = tokens
	lp.TokensByLevel[VisibilityDoc] = tokens
	lp.docComputed = true
}

// focusTestNamesSection lists the test function names of a focus
// package: top-level Test, Benchmark, Fuzz, and Example functions
// declared in the package's test files. Names — not bodies — are included
// in the focus documentation block so the model can discover and fetch
// potentially related test code with go-src blocks naming a function,
// without paying the token cost of every test body up front. Names are
// deduplicated and sorted for deterministic output. See
// TheoryOfVisibilityAllocation.
func focusTestNamesSection(lp *LogicalPackage) string {
	var names []string
	for _, f := range lp.Files {
		if !f.IsTestFile || f.AstFile == nil {
			continue
		}
		for _, decl := range f.AstFile.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil {
				continue
			}
			name := fn.Name.Name
			if strings.HasPrefix(name, "Test") ||
				strings.HasPrefix(name, "Benchmark") ||
				strings.HasPrefix(name, "Fuzz") ||
				strings.HasPrefix(name, "Example") {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	slices.Sort(names)
	names = slices.Compact(names)
	var b strings.Builder
	b.WriteString("\nTest functions in this package (fetch a test's source with a go-src block naming the function):\n")
	for _, name := range names {
		b.WriteString("- " + name + "\n")
	}
	return b.String()
}

// focusPackageReadOnly reports whether a focus package's files live
// outside the writable directories. Focus Go files are no longer emitted
// individually, so the per-file read-only annotation in
// PartsProvider.Parts cannot reach the focus documentation block; the
// note is carried in the block's begin marker instead, preserving the
// instruction that change blocks must not target read-only files. The
// first file decides; a check error is treated as not read-only. See
// anytexts.TheoryOfFocusFileDirectoryCheck.
func focusPackageReadOnly(lp *LogicalPackage) bool {
	for _, f := range lp.Files {
		outside, err := pathutil.IsOutsideWritableDirs(f.Path)
		if err != nil {
			return false
		}
		return outside
	}
	return false
}

// computePackageCosts renders and token-counts every file of the logical
// package at the code and full visibility levels, caching the result on
// the package. The computation is performed at most once per package:
// later calls return the cached result (or the cached error). The two
// documentation levels are not handled here — their costs are computed
// by computePackageShortDoc and computePackageDoc when the allocation
// considers those levels; the shortDocComputed and docComputed guards
// below ensure a real doc cost set by an earlier doc computation is never
// clobbered. Costs are computed lazily, driven by the allocation: focus
// and context packages are precomputed eagerly in
// precomputeTokenCounts because their costs are required up front, while
// all other packages are computed on demand only when probed. Packages
// that receive no visibility never run the tokenizer. See
// TheoryOfLazyVisibilityCosts.
func computePackageCosts(lp *LogicalPackage, countTokens func(string) (int, error)) error {
	if lp.costsComputed {
		return lp.costsErr
	}

	// The documentation-level budget costs carry the 1<<30 sentinel until
	// computePackageShortDoc or computePackageDoc overwrites them with the
	// real token counts, exactly when the package is considered for those
	// levels. The computed guards preserve a real doc cost when a doc
	// computation ran before this function (e.g., a package probed by the
	// water-fill before its file costs are needed). See
	// TheoryOfLazyPackageDoc.
	if !lp.shortDocComputed {
		lp.BudgetTokensByLevel[VisibilityShortDoc] = 1 << 30
		lp.TokensByLevel[VisibilityShortDoc] = 0
	}
	if !lp.docComputed {
		lp.BudgetTokensByLevel[VisibilityDoc] = 1 << 30
		lp.TokensByLevel[VisibilityDoc] = 0
	}

	fileRenders := make(map[*File]renderedFile, len(lp.Files))
	for _, f := range lp.Files {
		if !shouldIncludeFile(f, VisibilityAll) {
			continue
		}
		content, tokens, err := renderFileAtLevel(f, countTokens)
		if err != nil {
			lp.costsErr = err
			lp.costsComputed = true
			return err
		}
		fileRenders[f] = renderedFile{
			file:    f,
			content: content,
			tokens:  tokens,
		}
	}

	for level := VisibilityAll; level >= VisibilityCode; level-- {
		var files []renderedFile
		var allTokens int
		var budgetTokens int

		for _, f := range lp.Files {
			if !shouldIncludeFile(f, level) {
				continue
			}
			rf, ok := fileRenders[f]
			if !ok {
				continue
			}
			files = append(files, rf)
			allTokens += rf.tokens
			if !f.DoNotSimplify {
				budgetTokens += rf.tokens
			}
		}

		lp.RenderedFiles[level] = files
		lp.TokensByLevel[level] = allTokens
		lp.BudgetTokensByLevel[level] = budgetTokens
	}

	lp.costsComputed = true
	return nil
}

// renderFileAtLevel renders a single file at the given visibility level
// and returns the content (with markers) and token count. Uses raw disk
// content instead of formatting the AST.
func renderFileAtLevel(
	f *File,
	countTokens func(string) (int, error),
) (content string, tokens int, err error) {
	rawContent := f.Content
	if len(rawContent) == 0 {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return "", 0, err
		}
		rawContent = data
		f.Content = data
	}

	var buf bytes.Buffer
	if err := formatContentForPrompt(&buf, rawContent, f.PackageIsRoot, f.ReadOnly, f.Path); err != nil {
		return "", 0, err
	}

	content = buf.String()
	tokens, err = countTokens(content)
	if err != nil {
		return "", 0, err
	}
	return content, tokens, nil
}

// precomputeTokenCounts renders and token-counts files at the code and
// full visibility levels, in parallel, for the packages whose costs the
// visibility allocation requires up front: context packages (their
// code-level cost is the first minimum-visibility decision), any package
// containing files always emitted at full content — DoNotSimplify files
// (explicitly requested via -file) and the non-Go files of focus packages,
// which go doc cannot summarize — and, under -all-src, focus packages
// themselves, whose full-level costs the focus pin at the top of
// allocateVisibility consumes immediately. Focus packages pinned at full
// documentation need no file costs: their budget contribution is their
// documentation size, so focus source files are never rendered or
// token-counted unless a file is explicitly requested. All other packages
// have their costs computed lazily by allocateVisibility only when they
// are probed; packages that receive no visibility never run the
// tokenizer. Documentation-level costs are NOT computed here: they are
// computed by computePackageShortDoc and computePackageDoc, lazily, for
// packages that reach a documentation level. See TheoryOfLazyVisibilityCosts.
func precomputeTokenCounts(
	logicalPkgs []*LogicalPackage,
	allSrc bool,
	countTokens func(string) (int, error),
) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, min(runtime.NumCPU()*8, 32))
	errCh := make(chan error, 1)
	var errOnce sync.Once

	for _, lp := range logicalPkgs {
		if lp.Category != CategoryContext &&
			!(allSrc && lp.Category == CategoryFocus) &&
			!slices.ContainsFunc(lp.Files, func(f *File) bool {
				return f.DoNotSimplify || (lp.Category == CategoryFocus && !f.IsGoFile)
			}) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(lp *LogicalPackage) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					errOnce.Do(func() {
						select {
						case errCh <- fmt.Errorf("precompute panic: %v", r):
						default:
						}
					})
				}
			}()

			if err := computePackageCosts(lp, countTokens); err != nil {
				errOnce.Do(func() {
					select {
					case errCh <- err:
					default:
					}
				})
				return
			}
		}(lp)
	}

	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		return err
	}
	return nil
}

// prefetchPackageDocs launches full-doc computation concurrently for every
// package whose minimum visibility includes full documentation, with
// bounded concurrency. The minimum-visibility allocation probes each such
// package unconditionally — the doc cost must be known before affordability
// can be decided — so the probes are run in parallel to hide the go doc
// subprocess latency; the allocation's later computeDoc calls short-circuit
// via the docComputed guard. Packages whose minimum visibility is below
// full documentation are not prefetched: the water-fill probes their short
// doc or full doc only when the budget survives every higher-priority
// package, which is rare for the low-priority long tail. Short doc is
// never prefetched because no category has it as a minimum visibility.
// See TheoryOfLazyPackageDoc.
func prefetchPackageDocs(
	logicalPkgs []*LogicalPackage,
	dir string,
	envs Envs,
	countTokens func(string) (int, error),
) {
	var jobs []*LogicalPackage
	for _, lp := range logicalPkgs {
		if lp.MinVisibility == VisibilityDoc {
			jobs = append(jobs, lp)
		}
	}
	if len(jobs) == 0 {
		return
	}

	// Bound concurrency: each probe spawns a Go toolchain subprocess, and
	// spawning dozens at once would exhaust file descriptors and memory on
	// smaller machines.
	workers := min(runtime.NumCPU(), 8)
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for _, lp := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(lp *LogicalPackage) {
			defer wg.Done()
			defer func() { <-sem }()
			computePackageDoc(lp, dir, envs, countTokens)
		}(lp)
	}
	wg.Wait()
}

func allocateVisibility(
	logicalPkgs []*LogicalPackage,
	logger logs.Logger,
	debug Debug,
	computeShortDoc func(lp *LogicalPackage),
	computeDoc func(lp *LogicalPackage),
	computeCosts func(lp *LogicalPackage) error,
) error {
	// computeShortDoc, computeDoc, and computeCosts may be nil when
	// callers pre-populate the package costs (tests). Production wiring
	// always provides all three via SimplifyFiles. See
	// TheoryOfLazyPackageDoc and TheoryOfLazyVisibilityCosts.
	if computeShortDoc == nil {
		computeShortDoc = func(*LogicalPackage) {}
	}
	if computeDoc == nil {
		computeDoc = func(*LogicalPackage) {}
	}
	ensureCosts := func(lp *LogicalPackage) error {
		if computeCosts == nil {
			return nil
		}
		return computeCosts(lp)
	}

	// Focus packages are pinned at full documentation: their initial
	// context is the declaration surface (go doc -all -cmd -u) plus the
	// package's test function names, and the model fetches implementation
	// source on demand with go-src blocks. Pinning bounds the initial
	// context of large projects without sacrificing detail, and the
	// water-fill never upgrades pinned focus packages — upgrading would
	// reintroduce the full-source initial context this scheme exists to
	// avoid. The documentation cost is computed here because the context
	// budget derives from it (a no-op when prefetchPackageDocs already
	// computed it). Under -all-src (MinVisibility raised to VisibilityAll
	// by SimplifyFiles), focus packages are instead pinned at full source
	// including tests; the full-level costs are computed here because the
	// budget derives from them. See TheoryOfVisibilityAllocation.
	for _, lp := range logicalPkgs {
		if lp.Category != CategoryFocus {
			continue
		}
		if lp.MinVisibility == VisibilityAll {
			if err := ensureCosts(lp); err != nil {
				return err
			}
			lp.Visibility = VisibilityAll
			continue
		}
		computeDoc(lp)
		lp.Visibility = VisibilityDoc
	}

	// The context token budget is dynamic: it is derived from the total
	// token count of the focus packages at their pinned level —
	// documentation by default, full source under -all-src — as
	// focusTokens / 4, rounded to the nearest multiple of
	// contextTokenBudgetUnit, floored at one unit, so large focus surfaces
	// allocate proportionally larger budgets for context packages. The
	// computation is deterministic — identical focus packages produce an
	// identical budget — preserving the LLM prefix cache. See
	// TheoryOfVisibilityAllocation.
	focusTokens := 0
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			focusTokens += lp.TokensByLevel[lp.Visibility]
		}
	}
	remaining := calculateMaxContextTokens(focusTokens)

	// Start all non-focus packages at invisible, then upgrade to min
	// visibility in priority order as budget allows.
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			continue
		}
		lp.Visibility = VisibilityInvisible
	}

	// Allocate minimum visibility in priority order. Context packages
	// (explicitly requested via -ctx or -dep) are guaranteed their
	// minimum visibility: their cost is deducted from the budget
	// unconditionally, even when it exceeds the remaining budget. An
	// explicitly requested package must never be dropped in favor of its
	// own dependencies, which are discovered automatically. A package
	// that cannot afford its minimum visibility is left invisible but
	// does NOT block lower-priority packages: the budget is shared, and
	// every package gets an independent chance to reach its minimum.
	// This avoids the cascade where a single unaffordable package (e.g.,
	// a direct dependency whose go doc output exceeds the budget, or
	// whose go doc fails) blanks out the entire context. Priority still
	// governs the order of allocation: higher-priority packages are
	// processed first, so the most important packages are allocated
	// before the budget is exhausted. See TheoryOfVisibilityAllocation.
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			continue
		}
		minVis := lp.MinVisibility
		if minVis == VisibilityInvisible {
			continue
		}
		// Placing the package at full documentation requires its go doc
		// cost; compute it lazily, once per package. Packages that never
		// reach a documentation level skip the expensive go doc
		// subprocess entirely. Placing a context package at the code
		// level requires its file costs, which are precomputed eagerly;
		// the ensureCosts call below is therefore a no-op in production.
		// No category has short doc as its minimum visibility, so the
		// minimum-visibility allocation never computes short doc.
		// See TheoryOfLazyPackageDoc and TheoryOfLazyVisibilityCosts.
		if minVis == VisibilityDoc {
			computeDoc(lp)
		} else if minVis == VisibilityCode {
			if err := ensureCosts(lp); err != nil {
				return err
			}
		}
		cost := lp.BudgetTokensByLevel[minVis]
		if lp.Category == CategoryContext || cost <= remaining {
			lp.Visibility = minVis
			remaining -= cost
		}
	}

	if debug {
		logger.Info("visibility after min allocation",
			"remaining", remaining,
		)
	}

	// Water-fill: upgrade from high to low priority, one level at a time.
	// Each iteration finds the leftmost (highest priority) affordable
	// package and upgrades it by one level. The predecessor gate applies
	// only to the two documentation transitions (invisible→short doc and
	// short doc→full doc), which require expensive go doc probes;
	// upgrades to the code and full levels use precomputed costs and may
	// proceed even when the predecessor is stuck at a lower level, so an
	// unaffordable package cannot waste the remaining budget.
	// See TheoryOfVisibilityAllocation.
	for {
		// When the minimum-visibility allocation exhausted the budget
		// (remaining <= 0, which happens when a guaranteed context
		// package costs more than the budget), no upgrade is affordable:
		// probing packages would only spawn go doc subprocesses whose
		// results could not be shown. See TheoryOfVisibilityAllocation
		// and TheoryOfLazyPackageDoc.
		if remaining <= 0 {
			break
		}

		upgraded := false
		predecessorLevel := VisibilityAll

		for _, lp := range logicalPkgs {
			if lp.Category == CategoryFocus {
				predecessorLevel = lp.Visibility
				continue
			}

			currentLevel := lp.Visibility
			nextLevel := currentLevel + 1

			if nextLevel > VisibilityAll {
				predecessorLevel = currentLevel
				continue
			}

			// The predecessor gate applies only to the documentation
			// transitions, because they require expensive go doc probes.
			// A package whose predecessor is still at a lower level is
			// not probed: its doc could not be shown without violating
			// the priority ordering (higher-priority packages are shown
			// at least as fully). See TheoryOfLazyPackageDoc.
			if (nextLevel == VisibilityShortDoc || nextLevel == VisibilityDoc) && nextLevel > predecessorLevel {
				predecessorLevel = currentLevel
				continue
			}

			// Entering a documentation level requires the package's doc
			// cost at that level; compute it lazily, once per package.
			// Entering the code or full level requires the package's file
			// costs, computed lazily only when probed; packages that
			// receive no visibility never run the tokenizer.
			// See TheoryOfLazyPackageDoc and TheoryOfLazyVisibilityCosts.
			if nextLevel == VisibilityShortDoc {
				computeShortDoc(lp)
			} else if nextLevel == VisibilityDoc {
				computeDoc(lp)
			} else if nextLevel >= VisibilityCode {
				if err := ensureCosts(lp); err != nil {
					return err
				}
			}

			upgradeCost := lp.BudgetTokensByLevel[nextLevel] - lp.BudgetTokensByLevel[currentLevel]
			if upgradeCost > remaining {
				predecessorLevel = currentLevel
				continue
			}

			lp.Visibility = nextLevel
			remaining -= upgradeCost
			upgraded = true

			if debug {
				logger.Info("visibility upgraded",
					"package", lp.PkgPath,
					"from", currentLevel,
					"to", nextLevel,
					"cost", upgradeCost,
					"remaining", remaining,
				)
			}

			break // restart from highest priority
		}

		if !upgraded {
			break
		}
	}

	if debug {
		for _, lp := range logicalPkgs {
			logger.Info("visibility final",
				"package", lp.PkgPath,
				"category", lp.Category,
				"visibility", lp.Visibility,
				"tokens", lp.TokensByLevel[lp.Visibility],
			)
		}
	}
	return nil
}
