package codes

import (
	"fmt"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
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
	return "xml"
}

func (Module) DiffHandler(
	name DiffHandlerName,
	unified UnifiedDiff,
	logger logs.Logger,
) codetypes.DiffHandler {
	logger.Info("diff handler", "name", name)
	switch name {
	case "unified":
		return unified
	case "xml":
		return XmlDiffHandler{}
	case "":
		return DumbDiffHandler{}
	}
	panic(fmt.Errorf("unknown diff handler: %s", name))
}

type DumbDiffHandler struct{}

var _ codetypes.DiffHandler = DumbDiffHandler{}

func (d DumbDiffHandler) Functions() []*generators.Function {
	return nil
}

func (d DumbDiffHandler) SystemPrompt() string {
	return ""
}

func (d DumbDiffHandler) RestatePrompt() string {
	return ""
}
