package codes

import (
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/logs"
)

func (Module) DiffHandler(
	logger logs.Logger,
) codetypes.DiffHandler {
	return BoundaryDiffHandler{}
}
