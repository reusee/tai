package changes

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	Blocks     blocks.Module
}