package gocodes

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/reusee/tai/logs"
)

const TheoryOfLazyPackageDoc = `
Package documentation (go doc output) is computed lazily, only for packages
that actually reach visibility level 1. The eager approach ran go doc -all
-cmd for every non-focus package in the dependency graph during
precomputeTokenCounts, spawning one Go toolchain subprocess per package —
hundreds of processes for a typical project — even though most packages end
at level 0 (invisible) or at levels 2/3 (full code), where the doc output is
never used. The allocation algorithm only requires a package's doc cost when
it considers placing the package at level 1: in the minimum-visibility
allocation (MinVisibility == 1) and in the water-filling upgrade from level
0 to level 1. At exactly those decision points allocateVisibility invokes
computeDoc (via computePackageDoc), which runs go doc once, caches the
result on the package (docComputed, DocContent, DocTokens), and sets the
level-1 token costs. Until a package's doc is computed, its level-1 budget
cost carries the sentinel 1<<30, which makes level 1 unaffordable; the
sentinel is never read by a decision because computeDoc runs first whenever
level 1 is considered.

The minimum-visibility allocation probes every package whose minimum
visibility includes documentation (same-module and direct-import packages):
the doc cost must be known before affordability can be decided. These
unconditional probes are launched concurrently by prefetchPackageDocs with
bounded concurrency, hiding the per-subprocess latency that a serial probe
loop would incur; the allocation's own computeDoc calls then short-circuit
via the docComputed guard.

The water-fill phase gates the 0→1 upgrade on the immediate predecessor: a
package whose predecessor is still at level 0 is not probed, because its doc
could not be shown without violating the priority ordering. This keeps the
number of go doc subprocesses proportional to the number of packages that
can actually benefit from level 1, rather than the size of the dependency
graph.

When go doc fails for a package, the doc is treated as empty rather than
unaffordable: the level-1 cost is zero and the level-1 content is empty
(nothing is emitted at level 1), and the water-fill can still upgrade the
package to level 2 (code), whose costs are precomputed. This turns a
systemic go doc failure into a graceful degradation to code visibility
instead of making every such package permanently invisible.
`

const TheoryOfVisibilityAllocation = `
The context token budget for non-focus packages is dynamic: it is derived
from the total token count of the focus packages so that large
repositories receive a proportionally larger context budget. The budget
is focusTokens / 4, rounded to the nearest multiple of
contextTokenBudgetUnit (32K), with a floor at one unit. A repository
whose focus packages total 128K tokens gets a 32K context budget; a 200K
focus package gets a 64K budget. This scaling prevents a large focus
package from starving its dependencies and supporting packages, which
would degrade the model's ability to reason about cross-package
interactions. Small projects stay at the 32K floor, matching the
original fixed-budget behavior. The computation is deterministic:
identical focus packages always produce an identical budget, so context
files are simplified to the same level across requests with the same
focus, preserving the LLM prefix cache.

The visibility allocation uses a water-filling algorithm that upgrades
packages from their minimum visibility to higher levels as the budget
allows. Packages are processed in priority order (highest first). Each
step upgrades the leftmost (highest priority) affordable package by one
level. A package that cannot afford a level is skipped rather than
blocking lower-priority packages, so a single unaffordable package
cannot blank out the entire context: the budget is shared, and every
package gets an independent chance to reach its minimum visibility.

The minimum-visibility allocation processes packages in priority order
and gives each package its minimum visibility if it fits in the
remaining budget; unaffordable packages are left invisible and do not
affect subsequent packages. Priority still governs the order of
allocation, so high-priority packages are allocated before the budget
is exhausted.

The water-fill upgrade phase retains one predecessor gate: the upgrade
from level 0 to level 1 (package documentation) is blocked when the
immediately preceding package is at level 0, because that transition
requires an expensive go doc subprocess probe (see
TheoryOfLazyPackageDoc). Upgrades to level 2 and 3 use precomputed costs
and are never gated, so packages behind an unaffordable one can still be
upgraded all the way to full code. This means the construction
principle — higher-priority packages have at least as much visibility as
lower-priority ones — holds when every package is affordable at its
minimum, and degrades gracefully otherwise: visibility may be
non-monotonic only in the presence of unaffordable packages.

Focus packages are always at level 3 (all files) and do not count
against the context budget. Non-focus packages share the context token
budget, a hard limit: packages are initially invisible, then upgraded
to their minimum visibility in priority order as the budget allows,
then upgraded further by the water-filling algorithm within the
remaining budget.
`

const TheoryOfLazyVisibilityCosts = `
File token costs (rendered content and token counts at visibility levels 2
and 3) are computed lazily, driven by the visibility allocation. Only
packages whose costs the allocation requires up front are precomputed in
parallel: focus packages (their level-3 total determines the dynamic
context token budget), context packages (their level-2 cost is the first
minimum-visibility decision), and any package containing DoNotSimplify
files (which are always shown at full content regardless of the package's
visibility). Every other package computes its costs on demand, exactly when
the allocation probes it for a level that needs the file costs. Packages
that receive no visibility — typically the long tail of external
dependencies and standard library packages — never run the tokenizer, and
packages that stop at level 1 (documentation) never pay the cost of
counting their source files. The eagerly computed and on-demand values are
identical because token counting is deterministic, so the allocation
outcome is unchanged; laziness only avoids the work.

The water-fill's probe sequence bounds the on-demand computes
automatically: a package whose predecessor is stuck at level 0 is never
probed for level 1, and file costs are only probed for packages that
actually crossed the level-1 boundary. In a typical session with a 32K-96K
context budget and a large dependency graph, only the project's own
packages and a few high-priority dependencies are ever token-counted.
`

const TheoryOfRenderedFileCache = `
The rendered content of a file (including the begin/end markers) does not
depend on the visibility level; levels 2 and 3 differ only in which files
are included. Level 3 includes every file, and level 2 includes only
non-test Go files, so the level-3 file set is a superset of level 2's, and
a file's render is identical at both levels. computePackageCosts therefore
renders and token-counts each file exactly once and reuses the result
across levels, eliminating duplicate disk reads and tokenizer work per
package. The computation itself is lazy per package: only packages probed
by the visibility allocation — or focus/context packages, whose costs are
required up front — are rendered and counted at all. See
TheoryOfLazyVisibilityCosts.
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
// given visibility level.
func shouldIncludeFile(f *File, level VisibilityLevel) bool {
	switch level {
	case VisibilityInvisible:
		return false
	case VisibilityDoc, VisibilityCode:
		return f.IsGoFile && !f.IsTestFile
	case VisibilityAll:
		return true
	}
	return false
}

// renderPackageDoc runs `go doc -all -cmd` for the package and wraps
// the output with context package markers. Level 1 documentation is
// per-package, not per-file. If go doc fails, the caller treats the
// package as unaffordable at level 1 (cost set to 1<<30).
// The -u flag is deliberately omitted: including unexported symbols
// roughly doubles the doc output for most packages without adding
// API-level reference value.
func renderPackageDoc(
	pkgPath string,
	dir string,
	envs []string,
	countTokens func(string) (int, error),
) (content string, tokens int, err error) {
	cmd := exec.Command("go", "doc", "-all", "-cmd", pkgPath)
	cmd.Dir = dir
	// go doc must not modify go.sum: the load environment injects
	// -mod=mod (see TheoryOfModModEnv) so go list can update go.mod
	// when it is out of sync, but go doc would then re-add checksums
	// that go mod tidy removed, causing go.sum to churn. Stripping
	// -mod=mod leaves the go command's default -mod=readonly, which
	// fails instead of writing when a checksum is missing. See
	// TheoryOfGoDocReadonly.
	cmd.Env = withoutModModEnv(envs)
	output, err := cmd.Output()
	if err != nil {
		return "", 0, err
	}

	text := string(output)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
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

// computePackageDoc computes and caches the go doc output for a logical
// package. It is the production computeDoc used by allocateVisibility via
// SimplifyFiles. On success the level-1 budget cost is the doc token count.
// On failure the doc is treated as empty: level 1 costs nothing and emits
// nothing, and the water-fill can still upgrade the package to level 2
// (code), whose costs are precomputed. This prevents a single go doc
// failure from making the package permanently unaffordable. The docComputed
// guard makes the call idempotent: each package runs the go doc subprocess
// at most once. See TheoryOfLazyPackageDoc.
func computePackageDoc(
	lp *LogicalPackage,
	dir string,
	envs Envs,
	countTokens func(string) (int, error),
) {
	if lp.docComputed {
		return
	}
	content, tokens, err := renderPackageDoc(lp.PkgPath, dir, []string(envs), countTokens)
	if err != nil {
		// Treat a failed go doc as an empty doc: zero cost and zero
		// content, so the package emits nothing at level 1 but can be
		// water-filled to level 2 (code).
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

// computePackageCosts renders and token-counts every file of the logical
// package at visibility levels 2 and 3, caching the result on the package.
// The computation is performed at most once per package: later calls return
// the cached result (or the cached error). Level 1 (package documentation)
// costs are not handled here — they are computed by computePackageDoc when
// the allocation considers level 1; the docComputed guard below ensures a
// real doc cost set by an earlier computePackageDoc call is never
// clobbered. Costs are computed lazily, driven by the allocation: focus and
// context packages are precomputed eagerly in precomputeTokenCounts because
// their costs are required up front, while all other packages are computed
// on demand only when probed. Packages that receive no visibility never run
// the tokenizer. See TheoryOfLazyVisibilityCosts.
func computePackageCosts(lp *LogicalPackage, countTokens func(string) (int, error)) error {
	if lp.costsComputed {
		return lp.costsErr
	}

	// The level-1 budget cost carries the go doc sentinel until
	// computePackageDoc overwrites it with the real doc token count,
	// exactly when the package is considered for level 1. The docComputed
	// guard preserves a real doc cost when computePackageDoc ran before
	// this function (e.g., a package probed 0→1 by the water-fill before
	// its file costs are needed at the 1→2 probe). See
	// TheoryOfLazyPackageDoc.
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

// precomputeTokenCounts renders and token-counts files at visibility levels
// 2 and 3, in parallel, for the packages whose costs the visibility
// allocation requires up front: focus packages (their level-3 total
// determines the dynamic context budget), context packages (their level-2
// minimum-visibility cost is the first allocation decision), and any
// package containing DoNotSimplify files, which are always shown at level 3
// regardless of the package's visibility. All other packages have their
// costs computed lazily by allocateVisibility only when they are probed;
// packages that receive no visibility never run the tokenizer. Level 1
// (package documentation) costs are NOT computed here: they are computed by
// computePackageDoc, lazily, for packages that reach level 1.
// See TheoryOfLazyVisibilityCosts.
func precomputeTokenCounts(
	logicalPkgs []*LogicalPackage,
	countTokens func(string) (int, error),
) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, min(runtime.NumCPU()*8, 32))
	errCh := make(chan error, 1)
	var errOnce sync.Once

	for _, lp := range logicalPkgs {
		if lp.Category != CategoryFocus && lp.Category != CategoryContext &&
			!slices.ContainsFunc(lp.Files, func(f *File) bool { return f.DoNotSimplify }) {
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

// prefetchPackageDocs launches go doc computation concurrently for every
// package whose minimum visibility includes documentation (level 1), with
// bounded concurrency. The minimum-visibility allocation probes each such
// package unconditionally — the doc cost must be known before affordability
// can be decided — so the probes are run in parallel to hide the go doc
// subprocess latency; the allocation's later computeDoc calls short-circuit
// via the docComputed guard. Packages whose minimum visibility is below
// level 1 are not prefetched: the water-fill probes their docs only when
// the budget survives every higher-priority package, which is rare for the
// low-priority long tail. See TheoryOfLazyPackageDoc.
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
	computeDoc func(lp *LogicalPackage),
	computeCosts func(lp *LogicalPackage) error,
) error {
	// computeDoc and computeCosts may be nil when callers pre-populate the
	// package costs (tests). Production wiring always provides both via
	// SimplifyFiles. See TheoryOfLazyPackageDoc and
	// TheoryOfLazyVisibilityCosts.
	if computeDoc == nil {
		computeDoc = func(*LogicalPackage) {}
	}
	ensureCosts := func(lp *LogicalPackage) error {
		if computeCosts == nil {
			return nil
		}
		return computeCosts(lp)
	}

	// Focus packages are always at level 3
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			lp.Visibility = VisibilityAll
		}
	}

	// The context token budget is dynamic: it is derived from the total
	// token count of the focus packages (focusTokens / 4, rounded to the
	// nearest multiple of contextTokenBudgetUnit, floored at one unit),
	// so large repositories with big focus packages receive
	// proportionally larger budgets for context packages. The
	// computation is deterministic — identical focus packages produce an
	// identical budget — preserving the LLM prefix cache. See
	// TheoryOfVisibilityAllocation.
	focusTokens := 0
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			if err := ensureCosts(lp); err != nil {
				return err
			}
			focusTokens += lp.TokensByLevel[VisibilityAll]
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

	// Allocate minimum visibility in priority order. A package that
	// cannot afford its minimum visibility is left invisible but does
	// NOT block lower-priority packages: the budget is shared, and every
	// package gets an independent chance to reach its minimum. This
	// avoids the cascade where a single unaffordable package (e.g., a
	// direct dependency whose go doc output exceeds the budget, or whose
	// go doc fails) blanks out the entire context. Priority still
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
		// Placing the package at level 1 requires its go doc cost; compute
		// it lazily, once per package. Packages that never reach level 1
		// skip the expensive go doc subprocess entirely. Placing a context
		// package at level 2 requires its file costs, which are precomputed
		// eagerly; the ensureCosts call below is therefore a no-op in
		// production. See TheoryOfLazyPackageDoc and
		// TheoryOfLazyVisibilityCosts.
		if minVis == VisibilityDoc {
			computeDoc(lp)
		} else if minVis == VisibilityCode {
			if err := ensureCosts(lp); err != nil {
				return err
			}
		}
		cost := lp.BudgetTokensByLevel[minVis]
		if cost <= remaining {
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
	// only to the 0→1 (doc) transition, which requires an expensive go
	// doc probe; upgrades to level 2 and 3 use precomputed costs and may
	// proceed even when the predecessor is stuck at a lower level, so an
	// unaffordable package cannot waste the remaining budget.
	// See TheoryOfVisibilityAllocation.
	for {
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

			// The predecessor gate applies only to the 0→1 (doc)
			// transition, because that transition requires an expensive
			// go doc probe. A package whose predecessor is still at
			// level 0 is not probed: its doc could not be shown without
			// violating the priority ordering (higher-priority packages
			// are shown at least as fully). See TheoryOfLazyPackageDoc.
			if nextLevel == VisibilityDoc && nextLevel > predecessorLevel {
				predecessorLevel = currentLevel
				continue
			}

			// Entering level 1 requires the package's go doc cost; compute
			// it lazily, once per package. Entering level 2 or 3 requires
			// the package's file costs, computed lazily only when probed;
			// packages that receive no visibility never run the tokenizer.
			// See TheoryOfLazyPackageDoc and TheoryOfLazyVisibilityCosts.
			if nextLevel == VisibilityDoc {
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
