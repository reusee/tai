package gotools

import (
	"cmp"
	"errors"
	"go/token"
	"path/filepath"
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
go list invocation. Distances from root packages are computed via BFS over the
Imports graph populated by NeedDeps. All non-standard-library packages in the
dependency graph have their files discovered and parsed, with no distance limit
by default; the water-filling algorithm in visibility.go determines which
packages are visible based on the 32K context budget. Standard library packages
are resolved by the loader (they appear in the dependency graph) but their files
are excluded at collection time in GetFiles, because the model already knows the
standard library; see TheoryOfStdLibExclusion in files.go. Go file ASTs are
parsed in files.go via parser.ParseFile.
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
	workspace Workspace,
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
		// resolveLoadContext applies the workspace adjustments shared with
		// the type-checked reference loader. See TheoryOfWorkspace and
		// TheoryOfModModEnv.
		dir, patterns, loadEnvs := resolveLoadContext(loadDir, loadPatterns, workspace, envs)
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
			Env:   loadEnvs,
			Dir:   dir,
		}

		rootPkgs, err = packages.Load(config, patterns...)
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

// resolveLoadContext applies the load-directory, pattern, and environment
// adjustments shared by the context loader and the type-checked reference
// loader. In workspace mode it loads from the workspace root so that
// package resolution covers every module in the workspace, strips the
// -mod=mod flag the go command forbids in workspace mode (a safety net for
// envs supplied via config, which bypass the Envs provider's workspace-aware
// handling), and replaces the default "./..." pattern — rejected from a
// non-module workspace root — with one pattern per workspace module.
// See TheoryOfWorkspace and TheoryOfModModEnv.
func resolveLoadContext(
	loadDir LoadDir,
	loadPatterns LoadPatterns,
	workspace Workspace,
	envs Envs,
) (dir string, patterns []string, loadEnvs Envs) {
	dir = string(loadDir)
	patterns = loadPatterns
	loadEnvs = envs
	if workspace == "" {
		return
	}
	dir = string(workspace)
	loadEnvs = Envs(withoutModModEnv(envs))
	if len(loadPatterns) == 1 && loadPatterns[0] == "./..." {
		modules := workspaceModules(string(workspace))
		wsPatterns := make([]string, 0, len(modules))
		for _, moduleDir := range modules {
			rel, err := filepath.Rel(string(workspace), moduleDir)
			if err != nil {
				continue
			}
			wsPatterns = append(wsPatterns, "./"+filepath.ToSlash(rel)+"/...")
		}
		if len(wsPatterns) > 0 {
			patterns = wsPatterns
		}
	}
	return
}
