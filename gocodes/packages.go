package gocodes

import (
	"cmp"
	"errors"
	"go/token"
	"slices"
	"sync"

	"github.com/reusee/tai/logs"
	"golang.org/x/tools/go/packages"
)

const TheoryOfLightweightPackageLoading = `
The packages loader deliberately avoids the heaviest analysis modes. NeedSyntax
and NeedTypesInfo continue to be omitted so go/packages never retains full ASTs
or per-identifier types for every transitive dependency. NeedTypes is also
omitted: type-checking the full dependency graph is the dominant memory consumer
and triggered OOM on large projects even after syntax was dropped.

NeedDeps is intentionally omitted for the same class of reason. NeedDeps would
materialize every transitive dependency Package before distance filtering can
run, so peak memory scales with the unbounded import graph rather than with the
packages that will actually contribute file content. Instead, roots (and optional
context packages) are loaded with NeedImports only, then imports are expanded
iteratively up to MaxPackageDistanceFromRoot. Imports beyond that depth are not
loaded, keeping Package retention proportional to the filtered working set.
Distance calculation and packages.Visit only need this bounded graph: after each
expansion pass, Imports maps are rewired to the fully loaded packages within
depth and unfinished stubs outside the budget are dropped so the BFS cannot walk
past the loaded set. Standard-library packages (Module == nil) are not expanded
further unless IncludeStdLib is enabled, which matches the later file-inclusion
filter and avoids retaining the whole stdlib tree when it would never appear in
context. Go file ASTs remain parsed lazily in files.go via parser.ParseFile for
only the files within MaxPackageDistanceFromRoot. Omission of NeedTypes means
pkg.Types and pkg.TypesInfo remain nil; any consumer that needed them must
compute types on a smaller, local subset instead of loading them for the entire
dependency tree.
`

// packageLoadMode is the go/packages load mode used for each iterative load
// pass. NeedDeps is deliberately excluded; depth expansion is implemented by
// loadPackagesToDepth. See TheoryOfLightweightPackageLoading.
const packageLoadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedImports |
	packages.NeedForTest |
	packages.NeedModule |
	packages.NeedEmbedFiles |
	packages.NeedEmbedPatterns

// packages returned by the loader
// usually the one package that in the WorkingDir
type GetPackages = func() ([]*packages.Package, error)

type GetRootPackages GetPackages

type GetContextPackages GetPackages

type GetFileSet = func() (*token.FileSet, error)

func (Module) Packages(
	noTests NoTests,
	envs Envs,
	logger logs.Logger,
	loadDir LoadDir,
	loadPatterns LoadPatterns,
	contextPatterns ContextPatterns,
	maxDistance MaxPackageDistanceFromRoot,
	includeStdLib IncludeStdLib,
) (
	getRootPackages GetRootPackages,
	getContextPackages GetContextPackages,
	getFileSet GetFileSet,
) {

	fset := token.NewFileSet()
	var rootPkgs []*packages.Package
	var contextPkgs []*packages.Package
	var err error

	init := sync.OnceFunc(func() {
		// Omit NeedTypes and NeedDeps: full type-checking and unbounded
		// dependency materialization are the dominant OOM sources. Depth
		// is controlled by iterative expansion. See
		// TheoryOfLightweightPackageLoading.
		config := &packages.Config{
			Mode:  packageLoadMode,
			Tests: !bool(noTests),
			Env:   envs,
			Dir:   string(loadDir),
			Fset:  fset,
		}

		rootPkgs, err = loadPackagesToDepth(
			config,
			loadPatterns,
			int(maxDistance),
			bool(includeStdLib),
		)
		if err != nil {
			return
		}
		// Sort packages by import path for deterministic ordering across runs.
		// This guarantees that all downstream processing (BFS distance calculation,
		// file sorting, etc.) produces identical results, preserving the LLM prefix cache.
		slices.SortStableFunc(rootPkgs, func(a, b *packages.Package) int {
			return cmp.Compare(a.PkgPath, b.PkgPath)
		})

		if len(contextPatterns) > 0 {
			var err2 error
			contextPkgs, err2 = loadPackagesToDepth(
				config,
				contextPatterns,
				int(maxDistance),
				bool(includeStdLib),
			)
			if err2 != nil {
				err = errors.Join(err, err2)
			}
			// Sort context packages similarly for deterministic ordering.
			slices.SortStableFunc(contextPkgs, func(a, b *packages.Package) int {
				return cmp.Compare(a.PkgPath, b.PkgPath)
			})
		}

		var errs []error
		packages.Visit(append(rootPkgs, contextPkgs...), nil, func(pkg *packages.Package) {
			for _, pkgErr := range pkg.Errors {
				errs = append(errs, pkgErr)
			}
			if pkg.Module != nil && pkg.Module.Error != nil {
				errs = append(errs, errors.New(pkg.Module.Error.Err))
			}
		})
		if len(errs) > 0 {
			err = errors.Join(err, errors.Join(errs...))
		}

		logger.Info("packages",
			"root", len(rootPkgs),
			"context", len(contextPkgs),
			"max distance", maxDistance,
		)
	})

	getRootPackages = func() ([]*packages.Package, error) {
		init()
		return rootPkgs, err
	}

	getContextPackages = func() ([]*packages.Package, error) {
		init()
		return contextPkgs, err
	}

	getFileSet = func() (*token.FileSet, error) {
		init()
		return fset, err
	}

	return
}

// loadPackagesToDepth loads pattern packages and iteratively expands their
// imports up to maxDepth. Full NeedDeps materialization is avoided so peak
// memory stays proportional to the filtered working set. See
// TheoryOfLightweightPackageLoading.
func loadPackagesToDepth(
	config *packages.Config,
	patterns []string,
	maxDepth int,
	includeStdLib bool,
) ([]*packages.Package, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	roots, err := packages.Load(config, patterns...)
	if err != nil {
		return nil, err
	}
	if maxDepth < 0 {
		maxDepth = 0
	}

	// Key loaded packages by ID so test variants and normal packages remain
	// distinct while Imports maps can be rewired to the loaded instance.
	byID := make(map[string]*packages.Package, len(roots))
	distanceByID := make(map[string]int, len(roots))
	queue := make([]string, 0, len(roots))

	for _, pkg := range roots {
		if pkg == nil || pkg.ID == "" {
			continue
		}
		if _, ok := byID[pkg.ID]; ok {
			continue
		}
		byID[pkg.ID] = pkg
		distanceByID[pkg.ID] = 0
		queue = append(queue, pkg.ID)
	}

	for head := 0; head < len(queue); head++ {
		id := queue[head]
		pkg := byID[id]
		depth := distanceByID[id]
		if depth >= maxDepth {
			continue
		}
		// Stdlib packages never contribute focus context unless explicitly
		// requested; skip expansion so their own deps are not materialised.
		if pkg.Module == nil && !includeStdLib {
			continue
		}

		var missing []string
		seenMissing := make(map[string]bool)
		for _, imp := range pkg.Imports {
			if imp == nil || imp.ID == "" {
				continue
			}
			if _, loaded := byID[imp.ID]; loaded {
				continue
			}
			if seenMissing[imp.ID] {
				continue
			}
			// Cgo pseudo-package has no files to load.
			if imp.PkgPath == "C" {
				continue
			}
			seenMissing[imp.ID] = true
			missing = append(missing, imp.ID)
		}
		if len(missing) == 0 {
			continue
		}

		loaded, loadErr := packages.Load(config, missing...)
		if loadErr != nil {
			return nil, loadErr
		}
		for _, next := range loaded {
			if next == nil || next.ID == "" {
				continue
			}
			if _, ok := byID[next.ID]; ok {
				continue
			}
			byID[next.ID] = next
			distanceByID[next.ID] = depth + 1
			queue = append(queue, next.ID)
		}
	}

	// Point Imports at fully loaded packages within the depth budget and
	// drop stubs beyond it so packages.Visit and distance BFS cannot retain
	// or walk unloaded dependencies.
	for _, pkg := range byID {
		for path, imp := range pkg.Imports {
			if imp == nil || imp.ID == "" {
				continue
			}
			if full, ok := byID[imp.ID]; ok {
				pkg.Imports[path] = full
			} else {
				delete(pkg.Imports, path)
			}
		}
	}

	return roots, nil
}
