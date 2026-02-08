package gocodes

import (
	"errors"
	"go/token"
	"sync"

	"github.com/reusee/tai/logs"
	"golang.org/x/tools/go/packages"
)

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
		config := &packages.Config{
			Mode: packages.NeedName |
				packages.NeedFiles |
				packages.NeedImports |
				packages.NeedDeps |
				packages.NeedTypes |
				packages.NeedSyntax |
				packages.NeedTypesInfo |
				packages.NeedTypesSizes |
				packages.NeedForTest |
				packages.NeedModule |
				packages.NeedEmbedFiles |
				packages.NeedEmbedPatterns,
			Tests: !bool(noTests),
			Fset:  fset,
			Env:   envs,
			Dir:   string(loadDir),
		}

		rootPkgs, err = packages.Load(config, loadPatterns...)
		if err != nil {
			return
		}

		if len(contextPatterns) > 0 {
			var err2 error
			contextPkgs, err2 = packages.Load(config, contextPatterns...)
			if err2 != nil {
				err = errors.Join(err, err2)
			}
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
