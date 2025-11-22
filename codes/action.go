package codes

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/vars"
)

type Action interface {
	Name() string
	InitialPhase(cont generators.Phase) generators.Phase
	DefineCmds()
	InitialGenerator() (generators.Generator, error)
}

type ActionArgument string

var actionNameFlag string

var actionArgumentFlag ActionArgument

func (Module) AllActions(
	chat ActionChat,
	do ActionDo,
) []Action {
	return []Action{
		chat,
		do,
	}
}

func init() {
	dscope.New(
		new(Module),
		modes.ForProduction(),
	).Call(func(
		actions []Action,
	) {
		for _, action := range actions {
			action.DefineCmds()
		}
	})
}

func (Module) Action(
	logger logs.Logger,
	loader configs.Loader,
	allActions []Action,
	chat ActionChat,
) (
	action Action,
) {
	defer func() {
		logger.Info("action",
			"name", action.Name(),
			"details", action,
		)
	}()

	name := vars.FirstNonZero(
		actionNameFlag,
		configs.First[string](loader, "action"),
	)
	for _, a := range allActions {
		if a.Name() == name {
			action = a
			break
		}
	}

	if action == nil {
		action = chat
	}

	return
}

func (Module) ActionArgument(
	loader configs.Loader,
) ActionArgument {
	return vars.FirstNonZero(
		actionArgumentFlag,
		configs.First[ActionArgument](loader, "action_argument"),
	)
}
