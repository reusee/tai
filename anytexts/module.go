package anytexts

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
)

type Module struct {
	dscope.Module
	Generators generators.Module
}
