package blocks

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	Nets       nets.Module
}