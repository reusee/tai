package codes

import (
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/logs"
)

func (Module) DiffHandler(
	logger logs.Logger,
) changes.DiffHandler {
	return changes.BoundaryDiffHandler{}
}