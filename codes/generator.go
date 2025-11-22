package codes

import (
	"sync"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/vars"
)

var planGeneratorNameFlag = cmds.Var[string]("-plan-model")

var codeGeneratorNameFlag = cmds.Var[string]("-code-model")

type GetPlanGenerator func() (generators.Generator, error)

func (Module) GetPlanGenerator(
	get generators.GetGenerator,
	loader configs.Loader,
	getDefault generators.GetDefaultGenerator,
	logger logs.Logger,
) GetPlanGenerator {
	return sync.OnceValues(func() (generators.Generator, error) {
		name := vars.FirstNonZero(
			*planGeneratorNameFlag,
			configs.First[string](loader, "plan_model"),
		)
		if name != "" {
			logger.Info("plan model", "name", name)
			return get(name)
		}
		logger.Info("use default model for planning")
		return getDefault()
	})
}

type GetCodeGenerator func() (generators.Generator, error)

func (Module) GetCodeGenerator(
	get generators.GetGenerator,
	loader configs.Loader,
	getDefault generators.GetDefaultGenerator,
	logger logs.Logger,
) GetCodeGenerator {
	return sync.OnceValues(func() (generators.Generator, error) {
		name := vars.FirstNonZero(
			*codeGeneratorNameFlag,
			configs.First[string](loader, "code_model"),
		)
		if name != "" {
			logger.Info("code model", "name", name)
			return get(name)
		}
		logger.Info("use default model for coding")
		return getDefault()
	})
}

type GetDefaultGenerator func() (generators.Generator, error)

func (Module) GetDefaultGenerator(
	get generators.GetDefaultGenerator,
) GetDefaultGenerator {
	return sync.OnceValues(get)
}
