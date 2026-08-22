package gocodes

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/reusee/tai/logs"
)

const TheoryOfSimplification = `
Simplification uses package-level visibility levels instead of
file-level transforms. Each package is assigned a visibility level based
on its priority (category, distance, path) and the dynamic context budget
(see TheoryOfVisibilityAllocation in visibility.go).
Level VisibilityInvisible: invisible (deleted). Level VisibilityShortDoc:
a short package overview via go doc without -all (per-package; the
package comment and the top-level symbol index, a fraction of the full
documentation's size). Level VisibilityDoc: full package documentation
via go doc -all -cmd (per-package output, not per-file; the -u flag is
deliberately omitted for context packages so the reference stays focused
on exported symbols, and added for focus packages so the model sees the
complete surface of the packages it edits, alongside the package's test
function names). Level VisibilityCode: full Go code without test files
(raw file content). Level VisibilityAll: all files including tests,
non-Go files, and embed files (raw file content).

The water-filling algorithm upgrades packages from their minimum visibility
to higher levels as the budget allows, processing packages in priority order.
See TheoryOfVisibilityAllocation in visibility.go.

Focus packages are pinned at full documentation (documentation plus
test-function names) and do not count against the budget; the model fetches
focus implementation source on demand with go-src blocks. Focus files
explicitly requested via -file and non-Go focus files (which go doc
cannot summarize) are still emitted at full content. File ordering (see
TheoryOfFileOrdering in files.go) places stable context files first and
volatile focus files last, maximizing the common prefix between
consecutive requests for LLM prefix caching.
`

const TheoryOfTokenComposition = `
Token composition logging makes the context token budget observable. The
SimplifyFiles step logs the allocation view: focus package documentation
tokens (the pinned full-doc blocks from which the budget derives), the
dynamic context budget derived from them, and how the context packages
consume that budget by visibility level (short-doc packages, doc-only
packages, code-only packages, full packages). The CodeProvider.Parts step
logs the assembly view: how the final prompt token total is composed of
focus project files, context project files (focus-package documentation
blocks carry the context-file marker and are counted with context tokens
there), extra files from -file patterns, and package documentation from
-doc patterns. Together these logs let the user see at a glance whether
the token budget is dominated by focus files, context files, or
user-requested additions, and whether the dynamic context budget is
under- or over-allocated. See TheoryOfVisibilityAllocation.
`

type SimplifyFiles func(files []*File, maxTokens int, countTokens func(string) (int, error)) ([]*File, error)

func (Module) SimplifyFiles(
	getRootPackages GetRootPackages,
	getContextPackages GetContextPackages,
	getGitChangeCounts GetGitChangeCounts,
	logger logs.Logger,
	debug Debug,
	loadDir LoadDir,
	envs Envs,
	workspace Workspace,
) SimplifyFiles {
	return func(files []*File, maxTokens int, countTokens func(string) (int, error)) ([]*File, error) {
		rootPkgs, err := getRootPackages()
		if err != nil {
			return nil, err
		}
		contextPkgs, err := getContextPackages()
		if err != nil {
			return nil, err
		}

		// Identify test files by path suffix
		for _, f := range files {
			f.IsTestFile = strings.HasSuffix(f.Path, "_test.go")
		}

		// 1. Build logical packages from files
		logicalPkgs := buildLogicalPackages(files, rootPkgs, contextPkgs)

		// 2. Categorize each logical package
		categorizePackages(logicalPkgs, rootPkgs, contextPkgs)

		// 2.5. Compute recent git change counts for root-module files. A
		// logical package's change count is the sum over its files of the
		// commits within the most recent recentChangeCommitCount commits
		// that touched the file. compareFilesForOutput sorts root-module
		// files by ascending count, so the most-changed packages sit at
		// the very end of the root-module block, preserving the LLM prefix
		// cache when volatile files change. Counts are zero outside a git
		// repository, falling back to the deterministic package ordering.
		// See TheoryOfGitChangeOrdering.
		gitChangeCounts, err := getGitChangeCounts()
		if err != nil {
			return nil, err
		}
		for _, lp := range logicalPkgs {
			if !slices.ContainsFunc(lp.Files, func(f *File) bool {
				return f.ModuleIsRoot
			}) {
				continue
			}
			for _, f := range lp.Files {
				lp.ChangeCount += gitChangeCounts[f.Path]
			}
			for _, f := range lp.Files {
				f.ChangeCount = lp.ChangeCount
			}
		}

		// 3. Compute distances via BFS from focus packages
		computeDistances(logicalPkgs)

		// 4. Sort by priority (category, distance, path)
		sortPackagesByPriority(logicalPkgs)

		// 5. Pre-compute per-file token counts at the code and full
		// visibility levels for the packages whose costs the allocation
		// requires up front, concurrently: context packages and any
		// package containing files always emitted at full content
		// (DoNotSimplify files and the non-Go files of focus packages).
		// Focus packages pinned at full documentation need no file costs.
		// All other packages have their costs computed lazily only when
		// the allocation probes them; packages that receive no visibility
		// never run the tokenizer. See TheoryOfLazyVisibilityCosts in
		// visibility.go.
		if err := precomputeTokenCounts(logicalPkgs, countTokens); err != nil {
			return nil, err
		}

		// 6. Water-fill: allocate visibility levels within budget. The
		// computeShortDoc and computeDoc hooks delegate to
		// computePackageShortDoc and computePackageDoc, which run go doc
		// for a package exactly when the allocation considers placing it
		// at that documentation level, caching the result so packages that
		// never reach a documentation level skip the expensive subprocess
		// entirely. The minimum-visibility allocation probes every package
		// whose minimum visibility includes full documentation, so those
		// probes are launched concurrently by prefetchPackageDocs first,
		// hiding the subprocess latency; the hooks' calls then
		// short-circuit via the docComputed guard. Short doc is computed
		// only by the water-fill, because no category has it as a minimum
		// visibility. go doc runs from the load directory (or workspace
		// root in workspace mode) so it can resolve package import paths.
		// The computeCosts hook delegates to computePackageCosts, which
		// renders and token-counts a package's files only when the
		// allocation probes it. See TheoryOfLazyPackageDoc and
		// TheoryOfLazyVisibilityCosts in visibility.go.
		dir := string(loadDir)
		if workspace != "" {
			dir = string(workspace)
		}
		prefetchPackageDocs(logicalPkgs, dir, envs, countTokens)
		computeShortDoc := func(lp *LogicalPackage) {
			computePackageShortDoc(lp, dir, envs, countTokens)
		}
		computeDoc := func(lp *LogicalPackage) {
			computePackageDoc(lp, dir, envs, countTokens)
		}
		computeCosts := func(lp *LogicalPackage) error {
			return computePackageCosts(lp, countTokens)
		}
		if err := allocateVisibility(logicalPkgs, logger, debug, computeShortDoc, computeDoc, computeCosts); err != nil {
			return nil, err
		}

		// Log the context token composition: focus package tokens, the
		// dynamic context budget derived from them, and how the context
		// packages consume that budget by visibility level.
		// See TheoryOfTokenComposition.
		logTokenComposition(logger, logicalPkgs)

		// 7. Collect output files at their assigned visibility levels
		var result []*File
		for _, lp := range logicalPkgs {
			// Files always emitted at full content regardless of the
			// package's visibility level: files explicitly requested via
			// -file (DoNotSimplify) and the non-Go files of focus
			// packages, which go doc cannot summarize.
			renderedAtAll := make(map[*File]renderedFile)
			for _, rf := range lp.RenderedFiles[VisibilityAll] {
				renderedAtAll[rf.file] = rf
			}
			for _, f := range lp.Files {
				nonGoFocus := lp.Category == CategoryFocus && !f.IsGoFile
				if !f.DoNotSimplify && !nonGoFocus {
					continue
				}
				rf, ok := renderedAtAll[f]
				if !ok {
					continue
				}
				what := "full content (non-Go focus file)"
				if f.DoNotSimplify {
					what = fmt.Sprintf("visibility level %d (do not simplify)", VisibilityAll)
				}
				f.LogicalPkgPath = lp.PkgPath
				f.PackageDistanceFromRoot = lp.Distance
				f.Confirmed = &Transformed{
					What:      what,
					Content:   []byte(rf.content),
					NumTokens: rf.tokens,
				}
				result = append(result, f)
			}

			if lp.Visibility == VisibilityInvisible {
				continue
			}

			// The documentation levels emit a single synthetic entry with
			// the package's go doc output: short doc (go doc without -all)
			// or full doc (go doc -all -cmd). The synthetic file uses the
			// package path as its Path so compareFilesForOutput can sort
			// it correctly.
			if lp.Visibility == VisibilityShortDoc && lp.ShortDocContent != "" {
				what := fmt.Sprintf("visibility level %d (go doc short)", VisibilityShortDoc)
				if docFile := packageDocFile(lp, lp.ShortDocContent, lp.ShortDocTokens, what); docFile != nil {
					result = append(result, docFile)
				}
				continue
			}

			if lp.Visibility == VisibilityDoc && lp.DocContent != "" {
				docWhat := fmt.Sprintf("visibility level %d (go doc)", VisibilityDoc)
				if lp.Category == CategoryFocus {
					docWhat = fmt.Sprintf("visibility level %d (focus go doc -u)", VisibilityDoc)
				}
				if docFile := packageDocFile(lp, lp.DocContent, lp.DocTokens, docWhat); docFile != nil {
					result = append(result, docFile)
				}
				continue
			}

			// The code and full levels: per-file rendering with raw disk
			// content.
			renderedAtVisibility := make(map[*File]renderedFile)
			for _, rf := range lp.RenderedFiles[lp.Visibility] {
				renderedAtVisibility[rf.file] = rf
			}
			for _, f := range lp.Files {
				if f.DoNotSimplify {
					continue
				}
				rf, ok := renderedAtVisibility[f]
				if !ok {
					continue
				}
				f.LogicalPkgPath = lp.PkgPath
				f.PackageDistanceFromRoot = lp.Distance
				f.Confirmed = &Transformed{
					What:      fmt.Sprintf("visibility level %d", lp.Visibility),
					Content:   []byte(rf.content),
					NumTokens: rf.tokens,
				}
				result = append(result, f)
			}
		}

		// 8. Sort for output (prefix cache optimization)
		slices.SortStableFunc(result, compareFilesForOutput)

		return result, nil
	}
}

// packageDocFile builds the synthetic output entry for a package
// documentation level (short doc or full doc): a single File whose
// content is the package's go doc output. The synthetic file uses the
// package path as its Path so compareFilesForOutput can sort it
// correctly. It returns nil when the package has no files, in which case
// the caller emits nothing.
func packageDocFile(lp *LogicalPackage, content string, tokens int, what string) *File {
	if len(lp.Files) == 0 {
		return nil
	}
	moduleIsRoot := false
	moduleIsNil := true
	for _, f := range lp.Files {
		moduleIsRoot = f.ModuleIsRoot
		moduleIsNil = f.ModuleIsNil
		break
	}
	mainPkg := lp.MainPackage
	if mainPkg == nil && len(lp.Packages) > 0 {
		mainPkg = lp.Packages[0]
	}
	pkgPathDepth := len(strings.Split(lp.PkgPath, "/"))
	return &File{
		Path:                    lp.PkgPath,
		IsGoFile:                true,
		Content:                 []byte(content),
		Package:                 mainPkg,
		PackageIsRoot:           false,
		PackageDistanceFromRoot: lp.Distance,
		PackagePathDepth:        pkgPathDepth,
		Module:                  lp.Module,
		ModuleIsRoot:            moduleIsRoot,
		ModuleIsNil:             moduleIsNil,
		LogicalPkgPath:          lp.PkgPath,
		ChangeCount:             lp.ChangeCount,
		Confirmed: &Transformed{
			What:      what,
			Content:   []byte(content),
			NumTokens: tokens,
		},
	}
}

func compareFilesForOutput(a, b *File) int {
	// root module last — outermost grouping so that all non-root-module
	// files (dependencies, stdlib) form the stable prefix
	if !a.ModuleIsRoot && b.ModuleIsRoot {
		return -1
	} else if a.ModuleIsRoot && !b.ModuleIsRoot {
		return 1
	}

	// non-nil module last (nil modules like stdlib come first within
	// the non-root-module group)
	if a.ModuleIsNil && !b.ModuleIsNil {
		return -1
	} else if !a.ModuleIsNil && b.ModuleIsNil {
		return 1
	}

	// root package last — within each module group, context files
	// (non-root packages) precede focus files (root package)
	if !a.PackageIsRoot && b.PackageIsRoot {
		return -1
	} else if a.PackageIsRoot && !b.PackageIsRoot {
		return 1
	}

	// root-module files: fewer recent git changes first, so the
	// most-changed packages form the volatile suffix of the root-module
	// block. When a volatile file changes, the preceding stable
	// root-module and dependency content keeps its position, maximizing
	// LLM prefix cache reuse. This key runs after the root-package
	// grouping, so context files always precede focus files: a change to
	// any focus file never shifts context or dependency content. All
	// files of a logical package share the package's change count, so the
	// package-path and file-path keys below apply as tiebreakers. Counts
	// are zero outside a git repository, falling back to the deterministic
	// package ordering. See TheoryOfGitChangeOrdering.
	if a.ModuleIsRoot && b.ModuleIsRoot && a.ChangeCount != b.ChangeCount {
		return cmp.Compare(a.ChangeCount, b.ChangeCount)
	}

	// go files last
	if !a.IsGoFile && b.IsGoFile {
		return -1
	} else if a.IsGoFile && !b.IsGoFile {
		return 1
	}

	// low distance last
	if a.PackageDistanceFromRoot != b.PackageDistanceFromRoot {
		return -cmp.Compare(a.PackageDistanceFromRoot, b.PackageDistanceFromRoot)
	}

	// shallow package last
	if a.PackagePathDepth != b.PackagePathDepth {
		return -cmp.Compare(a.PackagePathDepth, b.PackagePathDepth)
	}

	// logical package path alphabetical
	if a.LogicalPkgPath != b.LogicalPkgPath {
		return cmp.Compare(a.LogicalPkgPath, b.LogicalPkgPath)
	}

	// file path alphabetical — primary stable key
	if a.Path != b.Path {
		return cmp.Compare(a.Path, b.Path)
	}

	// modification time — final tiebreaker
	if a.ModTime.Before(b.ModTime) {
		return -1
	} else if b.ModTime.Before(a.ModTime) {
		return 1
	}
	return 0
}

// logTokenComposition logs the context token composition after visibility
// allocation: focus package documentation tokens, the dynamic context
// budget derived from them, and how the context packages consume that
// budget by visibility level. The composition makes it possible to see at
// a glance whether the context budget is dominated by short-doc, doc-only,
// code-only, or full packages, and how many packages are invisible. See
// TheoryOfTokenComposition.
func logTokenComposition(
	logger logs.Logger,
	logicalPkgs []*LogicalPackage,
) {
	focusTokens := 0
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			focusTokens += lp.TokensByLevel[VisibilityDoc]
		}
	}
	var contextTokensByLevel [5]int
	var contextPackagesByLevel [5]int
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			continue
		}
		contextTokensByLevel[lp.Visibility] += lp.TokensByLevel[lp.Visibility]
		contextPackagesByLevel[lp.Visibility]++
	}
	contextTokens := contextTokensByLevel[VisibilityShortDoc] +
		contextTokensByLevel[VisibilityDoc] +
		contextTokensByLevel[VisibilityCode] +
		contextTokensByLevel[VisibilityAll]
	logger.Info("context token composition",
		"focus tokens", focusTokens,
		"context budget", calculateMaxContextTokens(focusTokens),
		"context tokens", contextTokens,
		"short doc packages", contextPackagesByLevel[VisibilityShortDoc],
		"doc packages", contextPackagesByLevel[VisibilityDoc],
		"code packages", contextPackagesByLevel[VisibilityCode],
		"full packages", contextPackagesByLevel[VisibilityAll],
		"invisible packages", contextPackagesByLevel[VisibilityInvisible],
		"short doc tokens", contextTokensByLevel[VisibilityShortDoc],
		"doc tokens", contextTokensByLevel[VisibilityDoc],
		"code tokens", contextTokensByLevel[VisibilityCode],
		"full tokens", contextTokensByLevel[VisibilityAll],
	)
}

// matchPattern reports whether the relative path matches the glob pattern,
// using doublestar.PathMatch for ** (globstar) support.
// See TheoryOfPatternMatching in anytexts/code_provider.go.
func matchPattern(name, pattern string) bool {
	matched, err := doublestar.PathMatch(pattern, name)
	return err == nil && matched
}
