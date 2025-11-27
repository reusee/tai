package anytexts

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/phases"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	Phases     phases.Module
}
