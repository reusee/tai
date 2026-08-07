package gocodes

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
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

const TheoryOfRenderedFileCache = `
The rendered content of a file (including the begin/end markers) does not
depend on the visibility level; levels 2 and 3 differ only in which files
are included. Level 3 includes every file, and level 2 includes only
non-test Go files, so the level-3 file set is a superset of level 2's, and
a file's render is identical at both levels. precomputeTokenCounts
therefore renders and token-counts each file exactly once and reuses the
result across levels, eliminating duplicate disk reads and tokenizer work
per package.
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
	cmd.Env = envs
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

// precomputeTokenCounts renders and token-counts each file at visibility
// levels 2 and 3 for every logical package, in parallel. Level 1 (package
// documentation via go doc) costs are NOT computed here: they are computed
// lazily by allocateVisibility only for packages that actually reach
// visibility level 1, because the go doc subprocess is expensive and most
// packages never use its output (they end at level 0 or at levels 2/3 where
// the full code is shown instead). See TheoryOfLazyPackageDoc.
func precomputeTokenCounts(
	logicalPkgs []*LogicalPackage,
	countTokens func(string) (int, error),
) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, min(runtime.NumCPU()*8, 32))
	errCh := make(chan error, 1)
	var errOnce sync.Once

	for _, lp := range logicalPkgs {
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

			// Level 1 (package documentation) costs are computed lazily by
			// allocateVisibility, only for packages that reach visibility
			// level 1. The sentinel 1<<30 makes level 1 unaffordable until
			// the doc is computed; packages that never reach level 1 skip
			// the expensive go doc subprocess entirely.
			// See TheoryOfLazyPackageDoc.
			lp.BudgetTokensByLevel[VisibilityDoc] = 1 << 30
			lp.TokensByLevel[VisibilityDoc] = 0

			// Levels 2 and 3: per-file rendering with raw disk content.
			// Render and token-count each file exactly once, then reuse the
			// result across levels. Level 3 includes every file and level 2
			// includes only non-test Go files, but the rendered content of a
			// file does not depend on the level, so rendering it twice would
			// duplicate I/O and tokenizer work. See
			// TheoryOfRenderedFileCache.
			fileRenders := make(map[*File]renderedFile, len(lp.Files))
			for _, f := range lp.Files {
				if !shouldIncludeFile(f, VisibilityAll) {
					continue
				}
				content, tokens, err := renderFileAtLevel(f, countTokens)
				if err != nil {
					errOnce.Do(func() {
						select {
						case errCh <- err:
						default:
						}
					})
					return
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
		}(lp)
	}

	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		return err
	}
	return nil
}

func allocateVisibility(
	logicalPkgs []*LogicalPackage,
	logger logs.Logger,
	debug Debug,
	computeDoc func(lp *LogicalPackage),
) {
	// computeDoc may be nil when callers pre-populate doc costs (tests).
	// Production wiring always provides it via SimplifyFiles.
	// See TheoryOfLazyPackageDoc.
	if computeDoc == nil {
		computeDoc = func(*LogicalPackage) {}
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
		// skip the expensive go doc subprocess entirely.
		// See TheoryOfLazyPackageDoc.
		if minVis == VisibilityDoc {
			computeDoc(lp)
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
			// it lazily, once per package. See TheoryOfLazyPackageDoc.
			if nextLevel == VisibilityDoc {
				computeDoc(lp)
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
}
