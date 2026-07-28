package loops

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/phases"
)

type Module struct {
	dscope.Module
	Blocks     blocks.Module
	Components components.Module
	Phases     phases.Module
}
