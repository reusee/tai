package gocodes

import (
	"go/types"

	"golang.org/x/tools/go/packages"
)

type GetPackagesMap func() (map[*types.Package]*packages.Package, error)

func (Module) PackagesMap(
	getRootPackages GetRootPackages,
	getContextPackages GetContextPackages,
) GetPackagesMap {
	return func() (map[*types.Package]*packages.Package, error) {
		rootPkgs, err := getRootPackages()
		if err != nil {
			return nil, err
		}
		contextPkgs, err := getContextPackages()
		if err != nil {
			return nil, err
		}
		ret := make(map[*types.Package]*packages.Package)
		packages.Visit(append(rootPkgs, contextPkgs...), nil, func(pkg *packages.Package) {
			ret[pkg.Types] = pkg
		})
		return ret, nil
	}
}
