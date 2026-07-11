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
The packages loader deliberately avoids the heaviest analysis modes. NeedSyntax,
NeedTypesInfo, and NeedTypes are omitted so go/packages never retains full ASTs,
per-identifier types, or complete type-checking results for any dependency.
NeedDeps is used so go/packages resolves the full dependency graph in a single
go list invocation. The prior approach of iteratively loading imports by explicit
PkgPath strings triggered go.mod consistency checks on every packages.Load call,
producing excessive go.mod computation overhead that outweighed the memory savings
of a smaller in-memory package set. Distances from root packages are computed via
BFS over the Imports graph populated by NeedDeps, and only packages within
MaxPackageDistanceFromRoot (default 2) have their files discovered and parsed.
Go file ASTs are parsed lazily in files.go via parser.ParseFile for only the files
within the distance bound.
`

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
		// NeedDeps loads the full dependency graph in a single go list
		// invocation. Packages beyond MaxPackageDistanceFromRoot are still
		// filtered out in Files() via BFS distance computation, but loading
		// them all at once avoids the go.mod overhead of multiple
		// packages.Load calls with explicit PkgPath.
		// See TheoryOfLightweightPackageLoading.
		config := &packages.Config{
			Mode: packages.NeedName |
				packages.NeedFiles |
				packages.NeedImports |
				packages.NeedDeps |
				packages.NeedForTest |
				packages.NeedModule |
				packages.NeedEmbedFiles |
				packages.NeedEmbedPatterns,
			Tests: !bool(noTests),
			Env:   envs,
			Dir:   string(loadDir),
		}

		rootPkgs, err = packages.Load(config, loadPatterns...)
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
			contextPkgs, err2 = packages.Load(config, contextPatterns...)
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
			for _, err := range pkg.Errors {
				errs = append(errs, err)
			}
			if pkg.Module != nil && pkg.Module.Error != nil {
				errs = append(errs, errors.New(pkg.Module.Error.Err))
			}
		})
		if len(errs) > 0 {
			err = errors.Join(err, errors.Join(errs...))
		}

		dirs := make(map[string]bool)
		for _, pkg := range append(rootPkgs, contextPkgs...) {
			if pkg.Module != nil {
				dirs[pkg.Module.Dir] = true
			}
		}

		logger.Info("packages", "root", len(rootPkgs), "context", len(contextPkgs))

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
