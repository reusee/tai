package main

import (
	"github.com/reusee/tai/generators"
)

type Generator struct {
	generators.Generator
}

func (Module) DefaultGenerator(
	get generators.GetDefaultGenerator,
) Generator {
	generator, err := get()
	ce(err)
	return Generator{
		Generator: generator,
	}
}
