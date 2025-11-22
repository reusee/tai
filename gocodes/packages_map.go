package gocodes

import (
	"go/types"

	"golang.org/x/tools/go/packages"
)

type GetPackagesMap func() (map[*types.Package]*packages.Package, error)

func (Module) PackagesMap(
	getPackages GetPackages,
) GetPackagesMap {
	return func() (map[*types.Package]*packages.Package, error) {
		pkgs, err := getPackages()
		if err != nil {
			return nil, err
		}
		ret := make(map[*types.Package]*packages.Package)
		packages.Visit(pkgs, nil, func(pkg *packages.Package) {
			ret[pkg.Types] = pkg
		})
		return ret, nil
	}
}
