package gocodes

import (
	"bytes"
	"fmt"
	"go/token"
	"runtime"
	"sync"

	"github.com/reusee/tai/logs"
)

const TheoryOfVisibilityAllocation = `
The visibility allocation uses a water-filling algorithm that upgrades
packages from their minimum visibility to higher levels as the budget
allows. Packages are processed in priority order (highest first). Each
step upgrades the leftmost (highest priority) affordable package by one
level, subject to the predecessor constraint: a package's visibility
must not exceed the previous package's visibility in priority order.
This ensures the construction principle: if package A has higher priority
than package B, A's visibility is not lower than B's.

Focus packages are always at level 3 (all files) and do not count
against the 32K context budget. Non-focus packages share the 32K budget.
The budget is fixed to ensure context files are simplified consistently
across requests, preserving the LLM prefix cache.
`

const maximumContextTokenBudget = 32 << 10

// calculateMaxContextTokens returns the fixed token budget for context
// (non-focus) files. The budget is constant to ensure context files are
// simplified to the same level every request, preserving the LLM prefix
// cache.
func calculateMaxContextTokens() int {
	return maximumContextTokenBudget
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

// renderFileAtLevel renders a single file at the given visibility level
// and returns the content (with markers) and token count.
func renderFileAtLevel(
	f *File,
	level VisibilityLevel,
	fset *token.FileSet,
	countTokens func(string) (int, error),
) (content string, tokens int, err error) {
	isRoot := f.PackageIsRoot
	readOnly := f.ReadOnly

	var buf bytes.Buffer

	switch level {
	case VisibilityDoc:
		// Level 1: Go documentation — delete function bodies, keep comments.
		// Run goimports (skipImports=false) to remove imports that became
		// unused after function body deletion.
		simplified := deleteFunctionBody(f.AstFile)
		if err := formatASTForPrompt(&buf, simplified, fset, isRoot, readOnly, f.Path, false); err != nil {
			return "", 0, err
		}

	case VisibilityCode:
		// Level 2: Full Go code without test files.
		// Skip goimports since the AST is unmodified from the parsed source.
		if err := formatASTForPrompt(&buf, f.AstFile, fset, isRoot, readOnly, f.Path, true); err != nil {
			return "", 0, err
		}

	case VisibilityAll:
		// Level 3: All files including tests, non-Go, and embed files.
		if f.IsGoFile {
			if err := formatASTForPrompt(&buf, f.AstFile, fset, isRoot, readOnly, f.Path, true); err != nil {
				return "", 0, err
			}
		} else {
			if err := formatContentForPrompt(&buf, f.Content, isRoot, readOnly, f.Path); err != nil {
				return "", 0, err
			}
		}
	}

	content = buf.String()
	tokens, err = countTokens(content)
	if err != nil {
		return "", 0, err
	}
	return content, tokens, nil
}

// precomputeTokenCounts concurrently renders each logical package at each
// visibility level and stores the results. See TheoryOfVisibilityAllocation.
//
// Levels are rendered from high to low (3→1) so that levels 2 and 3, which
// use the original AST directly, are rendered before level 1, which calls
// deleteFunctionBody. This prevents any potential AST mutation from
// corrupting the higher-level renderings.
func precomputeTokenCounts(
	logicalPkgs []*LogicalPackage,
	fset *token.FileSet,
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

			// Render from high to low level so that levels 2 and 3 use
			// the original (unmodified) AST before level 1 potentially
			// mutates it via deleteFunctionBody.
			for level := VisibilityAll; level >= VisibilityDoc; level-- {
				var files []renderedFile
				var allTokens int
				var budgetTokens int

				for _, f := range lp.Files {
					if !shouldIncludeFile(f, level) {
						continue
					}
					content, tokens, err := renderFileAtLevel(f, level, fset, countTokens)
					if err != nil {
						errOnce.Do(func() {
							select {
							case errCh <- err:
							default:
							}
						})
						return
					}
					files = append(files, renderedFile{
						file:    f,
						content: content,
						tokens:  tokens,
					})
					allTokens += tokens
					if !f.DoNotSimplify {
						budgetTokens += tokens
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

// allocateVisibility assigns visibility levels to logical packages using
// the water-filling algorithm. See TheoryOfVisibilityAllocation.
func allocateVisibility(
	logicalPkgs []*LogicalPackage,
	logger logs.Logger,
	debug Debug,
) {
	// Focus packages are always at level 3
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			lp.Visibility = VisibilityAll
		}
	}

	// Compute initial cost for non-focus packages (excluding DoNotSimplify)
	var totalCost int
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			continue
		}
		totalCost += lp.BudgetTokensByLevel[lp.MinVisibility]
	}

	remaining := maximumContextTokenBudget - totalCost
	if remaining < 0 {
		remaining = 0
	}

	if debug {
		logger.Info("visibility allocation start",
			"initial cost", totalCost,
			"budget", maximumContextTokenBudget,
			"remaining", remaining,
		)
	}

	// Water-fill: upgrade from high to low priority, one level at a time.
	// Each iteration finds the leftmost (highest priority) affordable
	// package and upgrades it by one level. The predecessor constraint
	// ensures visibility is non-increasing in priority order.
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

			// Predecessor constraint: must not exceed previous package's level
			if nextLevel > predecessorLevel {
				predecessorLevel = currentLevel
				continue
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
