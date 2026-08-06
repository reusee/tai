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
Simplification uses package-level visibility levels (0-3) instead of
file-level transforms. Each package is assigned a visibility level based
on its priority (category, distance, path) and the 32K context budget.
Level 0: invisible (deleted). Level 1: package documentation via
go doc -all -cmd -u (per-package output, not per-file). Level 2: full
Go code without test files (raw file content from disk). Level 3: all
files including tests, non-Go files, and embed files (raw file content
from disk).

The water-filling algorithm upgrades packages from their minimum visibility
to higher levels as the budget allows, processing packages in priority order.
See TheoryOfVisibilityAllocation in visibility.go.

Focus packages are always at level 3 and do not count against the budget.
File ordering (see TheoryOfFileOrdering in files.go) places stable context
files first and volatile focus files last, maximizing the common prefix
between consecutive requests for LLM prefix caching.
`

type SimplifyFiles func(files []*File, maxTokens int, countTokens func(string) (int, error)) ([]*File, error)

func (Module) SimplifyFiles(
	getRootPackages GetRootPackages,
	getContextPackages GetContextPackages,
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

		// 3. Compute distances via BFS from focus packages
		computeDistances(logicalPkgs)

		// 4. Sort by priority (category, distance, path)
		sortPackagesByPriority(logicalPkgs)

		// 5. Pre-compute token counts at each visibility level concurrently.
		// go doc runs from the load directory (or workspace root in
		// workspace mode) so it can resolve package import paths.
		dir := string(loadDir)
		if workspace != "" {
			dir = string(workspace)
		}
		if err := precomputeTokenCounts(logicalPkgs, countTokens, dir, []string(envs)); err != nil {
			return nil, err
		}

		// 6. Water-fill: allocate visibility levels within budget
		allocateVisibility(logicalPkgs, logger, debug)

		// 7. Collect output files at their assigned visibility levels
		var result []*File
		for _, lp := range logicalPkgs {
			// DoNotSimplify files are always at level 3 (full content),
			// regardless of the package's visibility level.
			renderedAtAll := make(map[*File]renderedFile)
			for _, rf := range lp.RenderedFiles[VisibilityAll] {
				renderedAtAll[rf.file] = rf
			}
			for _, f := range lp.Files {
				if !f.DoNotSimplify {
					continue
				}
				rf, ok := renderedAtAll[f]
				if !ok {
					continue
				}
				f.LogicalPkgPath = lp.PkgPath
				f.PackageDistanceFromRoot = lp.Distance
				f.Confirmed = &Transformed{
					What:      "visibility level 3 (do not simplify)",
					Content:   []byte(rf.content),
					NumTokens: rf.tokens,
				}
				result = append(result, f)
			}

			if lp.Visibility == VisibilityInvisible {
				continue
			}

			// Level 1: emit a single synthetic entry with go doc output.
			// The synthetic file uses the package path as its Path so
			// compareFilesForOutput can sort it correctly.
			if lp.Visibility == VisibilityDoc && lp.DocContent != "" {
				if len(lp.Files) == 0 {
					continue
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
				docFile := &File{
					Path:                    lp.PkgPath,
					IsGoFile:                true,
					Content:                 []byte(lp.DocContent),
					Package:                 mainPkg,
					PackageIsRoot:           false,
					PackageDistanceFromRoot: lp.Distance,
					PackagePathDepth:        pkgPathDepth,
					Module:                  lp.Module,
					ModuleIsRoot:            moduleIsRoot,
					ModuleIsNil:             moduleIsNil,
					LogicalPkgPath:          lp.PkgPath,
					Confirmed: &Transformed{
						What:      "visibility level 1 (go doc)",
						Content:   []byte(lp.DocContent),
						NumTokens: lp.DocTokens,
					},
				}
				result = append(result, docFile)
				continue
			}

			// Levels 2 and 3: per-file rendering with raw disk content.
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

// compareFilesForOutput implements the output ordering for prefix cache
// optimization. See TheoryOfFileOrdering in files.go.
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

// matchPattern reports whether the relative path matches the glob pattern,
// using doublestar.PathMatch for ** (globstar) support.
// See TheoryOfPatternMatching in anytexts/code_provider.go.
func matchPattern(name, pattern string) bool {
	matched, err := doublestar.PathMatch(pattern, name)
	return err == nil && matched
}
