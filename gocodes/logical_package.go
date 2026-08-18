package gocodes

import (
	"cmp"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

const TheoryOfLogicalPackages = `
Logical packages merge the main package with its test variants produced by
packages.Load with Tests:true. When Tests is enabled, a package "foo" may
appear as "foo" (main) and "foo [foo.test]" (test variant). These are merged
into a single logical package with PkgPath "foo". The external test package
"foo_test" is a separate logical package. Level 3 includes _test.go files;
levels 0-2 exclude them. The distance graph uses merged logical packages.

Package categorization determines the minimum visibility and priority:
- Focus packages (from -pkg): level 3, always visible
- Context packages (from -ctx, -dep): level 2, always visible
- Same-module non-focus packages: level 1
- Direct imports of focus packages: level 1
- Other module packages: level 0
- Standard library: excluded at collection time, so it never forms a logical
  package unless explicitly requested via -pkg or -ctx (in which case it is
  categorized as focus or context). See TheoryOfStdLibExclusion in files.go.

Priority ordering: category (higher first), distance (shorter first),
package path (ascending). The water-filling algorithm upgrades packages
from their minimum visibility to higher levels as the budget allows.
See TheoryOfVisibilityAllocation in visibility.go.
`

// VisibilityLevel represents how much of a package's content is visible.
type VisibilityLevel int

const (
	VisibilityInvisible VisibilityLevel = 0
	VisibilityDoc       VisibilityLevel = 1
	VisibilityCode      VisibilityLevel = 2
	VisibilityAll       VisibilityLevel = 3
)

// PackageCategory represents the major classification of a package,
// determining its minimum visibility and priority ordering.
// Categories are listed in priority order (highest first).
type PackageCategory int

const (
	CategoryFocus        PackageCategory = iota // focus packages (from -pkg)
	CategoryContext                             // packages from -ctx, -dep
	CategorySameModule                          // same module as focus, non-focus
	CategoryDirectImport                        // directly imported by focus
	CategoryOtherModule                         // in module of non-focus package
	CategoryStdLib                              // standard library
)

// categoryMinVisibility returns the minimum visibility for a category.
func categoryMinVisibility(c PackageCategory) VisibilityLevel {
	switch c {
	case CategoryFocus:
		return VisibilityAll
	case CategoryContext:
		return VisibilityCode
	case CategorySameModule, CategoryDirectImport:
		return VisibilityDoc
	default:
		return VisibilityInvisible
	}
}

// renderedFile holds the rendered content and token count for a single
// file at a specific visibility level.
type renderedFile struct {
	file    *File
	content string
	tokens  int
}

type LogicalPackage struct {
	PkgPath     string
	Packages    []*packages.Package
	MainPackage *packages.Package
	Module      *packages.Module

	Files []*File

	Category      PackageCategory
	Distance      int
	MinVisibility VisibilityLevel
	Visibility    VisibilityLevel
	ChangeCount   int

	// Pre-computed rendered files and token counts at each visibility level.
	RenderedFiles [4][]renderedFile
	TokensByLevel [4]int

	// BudgetTokensByLevel excludes DoNotSimplify files from the token count,
	// because DoNotSimplify files are always at level 3 and do not count
	// against the 32K context budget.
	BudgetTokensByLevel [4]int

	// DocContent and DocTokens hold the go doc output for level 1
	// (VisibilityDoc). Level 1 is per-package, not per-file.
	DocContent string
	DocTokens  int

	// docComputed reports whether the package's go doc output has been
	// computed. Doc computation is lazy: only packages that reach
	// visibility level 1 run the go doc subprocess, exactly once.
	// See TheoryOfLazyPackageDoc in visibility.go.
	docComputed bool

	// costsComputed reports whether RenderedFiles, TokensByLevel, and
	// BudgetTokensByLevel for visibility levels 2 and 3 have been populated
	// for this package; costsErr records a failure so the computation is
	// attempted at most once. Costs are computed lazily, driven by the
	// visibility allocation: focus and context packages are precomputed
	// eagerly, while every other package is computed on demand only when
	// probed. Packages that receive no visibility never run the tokenizer.
	// See TheoryOfLazyVisibilityCosts in visibility.go.
	costsComputed bool
	costsErr      error

	rootPkgSet    bool
	contextPkgSet bool
}

// basePkgPath strips the variant suffix from a package path.
// "foo [foo.test]" -> "foo", "foo" -> "foo"
func basePkgPath(pkgPath string) string {
	if i := strings.Index(pkgPath, " ["); i >= 0 {
		return pkgPath[:i]
	}
	return pkgPath
}

// buildLogicalPackages groups files into logical packages by base PkgPath.
// Files are associated with their package's logical package. Packages that
// appear in rootPkgs or contextPkgs but have no files are also included
// (they may contribute to the distance graph).
func buildLogicalPackages(
	files []*File,
	rootPkgs []*packages.Package,
	contextPkgs []*packages.Package,
) []*LogicalPackage {
	byPath := make(map[string]*LogicalPackage)
	var order []string

	getOrCreate := func(pkg *packages.Package) *LogicalPackage {
		base := basePkgPath(pkg.PkgPath)
		lp, ok := byPath[base]
		if !ok {
			lp = &LogicalPackage{
				PkgPath: base,
			}
			byPath[base] = lp
			order = append(order, base)
		}
		lp.Packages = append(lp.Packages, pkg)
		if pkg.PkgPath == base {
			lp.MainPackage = pkg
		}
		if lp.Module == nil && pkg.Module != nil {
			lp.Module = pkg.Module
		}
		return lp
	}

	for _, pkg := range rootPkgs {
		lp := getOrCreate(pkg)
		lp.rootPkgSet = true
	}

	for _, pkg := range contextPkgs {
		base := basePkgPath(pkg.PkgPath)
		if lp, ok := byPath[base]; ok && lp.rootPkgSet {
			continue // focus takes precedence
		}
		lp := getOrCreate(pkg)
		lp.contextPkgSet = true
	}

	// Assign files to logical packages
	for _, f := range files {
		if f.Package == nil {
			continue
		}
		base := basePkgPath(f.Package.PkgPath)
		lp, ok := byPath[base]
		if !ok {
			lp = &LogicalPackage{
				PkgPath:     base,
				MainPackage: f.Package,
				Module:      f.Module,
			}
			byPath[base] = lp
			order = append(order, base)
		}
		lp.Files = append(lp.Files, f)
	}

	// Build result in deterministic order
	result := make([]*LogicalPackage, 0, len(order))
	for _, path := range order {
		result = append(result, byPath[path])
	}
	return result
}

// categorizePackages determines the category of each logical package based
// on its relationship to focus and context packages. See TheoryOfLogicalPackages.
func categorizePackages(
	logicalPkgs []*LogicalPackage,
	rootPkgs []*packages.Package,
	contextPkgs []*packages.Package,
) {
	// Collect root module paths (modules of focus and context packages)
	rootModulePaths := make(map[string]bool)
	for _, pkg := range rootPkgs {
		if pkg.Module != nil {
			rootModulePaths[pkg.Module.Path] = true
		}
	}
	for _, pkg := range contextPkgs {
		if pkg.Module != nil {
			rootModulePaths[pkg.Module.Path] = true
		}
	}

	// Collect direct imports of focus packages
	focusDirectImports := make(map[string]bool)
	for _, pkg := range rootPkgs {
		for _, imp := range pkg.Imports {
			if imp == nil {
				continue
			}
			focusDirectImports[basePkgPath(imp.PkgPath)] = true
		}
	}

	for _, lp := range logicalPkgs {
		switch {
		case lp.rootPkgSet:
			lp.Category = CategoryFocus
		case lp.contextPkgSet:
			lp.Category = CategoryContext
		case lp.Module != nil && rootModulePaths[lp.Module.Path]:
			lp.Category = CategorySameModule
		case focusDirectImports[lp.PkgPath]:
			lp.Category = CategoryDirectImport
		case lp.Module == nil:
			lp.Category = CategoryStdLib
		default:
			lp.Category = CategoryOtherModule
		}
		lp.MinVisibility = categoryMinVisibility(lp.Category)
		lp.Visibility = lp.MinVisibility
	}
}

// computeDistances computes the shortest import distance from any focus
// package to each logical package via BFS on the merged dependency graph.
func computeDistances(logicalPkgs []*LogicalPackage) {
	byPath := make(map[string]*LogicalPackage)
	for _, lp := range logicalPkgs {
		byPath[lp.PkgPath] = lp
		lp.Distance = -1
	}

	var queue []*LogicalPackage
	for _, lp := range logicalPkgs {
		if lp.Category == CategoryFocus {
			lp.Distance = 0
			queue = append(queue, lp)
		}
	}

	for len(queue) > 0 {
		lp := queue[0]
		queue = queue[1:]

		// Collect imports from all variant packages
		seen := make(map[string]bool)
		for _, pkg := range lp.Packages {
			for _, imp := range pkg.Imports {
				if imp == nil {
					continue
				}
				seen[basePkgPath(imp.PkgPath)] = true
			}
		}

		for impPath := range seen {
			impLp, ok := byPath[impPath]
			if !ok {
				continue
			}
			if impLp.Distance == -1 {
				impLp.Distance = lp.Distance + 1
				queue = append(queue, impLp)
			}
		}
	}

	// Set large distance for unreachable packages
	for _, lp := range logicalPkgs {
		if lp.Distance == -1 {
			lp.Distance = 1 << 30
		}
	}
}

// sortPackagesByPriority sorts logical packages by priority:
// 1. Category (lower value = higher priority, Focus first)
// 2. Distance (shorter = higher priority)
// 3. Package path (ascending)
func sortPackagesByPriority(logicalPkgs []*LogicalPackage) {
	slices.SortStableFunc(logicalPkgs, func(a, b *LogicalPackage) int {
		if a.Category != b.Category {
			return cmp.Compare(a.Category, b.Category)
		}
		if a.Distance != b.Distance {
			return cmp.Compare(a.Distance, b.Distance)
		}
		return cmp.Compare(a.PkgPath, b.PkgPath)
	})
}
