package main

import "github.com/reusee/tai/generators"

func (Module) Generator(
	getDefaultGenerator generators.GetDefaultGenerator,
) generators.Generator {
	ret, err := getDefaultGenerator()
	ce(err)
	return ret
}
