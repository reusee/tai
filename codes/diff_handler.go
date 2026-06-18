package codes

import (
	"fmt"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/vars"
)

type DiffHandlerName string

var diffHandlerName = cmds.Var[DiffHandlerName]("-diff")

func (Module) DiffHandlerName(
	defaultName DefaultDiffHandlerName,
) DiffHandlerName {
	return vars.FirstNonZero(
		*diffHandlerName,
		DiffHandlerName(defaultName),
	)
}

type DefaultDiffHandlerName DiffHandlerName

func (Module) DefaultDiffHandlerName() DefaultDiffHandlerName {
	return "boundary"
}

func (Module) DiffHandler(
	name DiffHandlerName,
	logger logs.Logger,
) codetypes.DiffHandler {
	logger.Info("diff handler", "name", name)
	switch name {
	case "boundary", "":
		return BoundaryDiffHandler{}
	}
	panic(fmt.Errorf("unknown diff handler: %s", name))
}
