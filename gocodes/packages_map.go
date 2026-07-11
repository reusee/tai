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
		// NeedTypes is intentionally not loaded (see TheoryOfLightweightPackageLoading),
		// so pkg.Types is always nil. Return an empty map rather than walking a
		// type graph that no longer exists, avoiding accidental reintroduction of
		// global type-checking.
		rootPkgs, err := getRootPackages()
		if err != nil {
			return nil, err
		}
		contextPkgs, err := getContextPackages()
		if err != nil {
			return nil, err
		}
		_ = rootPkgs
		_ = contextPkgs
		return map[*types.Package]*packages.Package{}, nil
	}
}
