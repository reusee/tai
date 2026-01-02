package generators

import (
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/vars"
)

type GetDefaultGenerator func() (Generator, error)

func (Module) GetDefaultGenerator(
	name DefaultModelName,
	get GetGenerator,
) GetDefaultGenerator {
	return func() (Generator, error) {
		return get(string(name))
	}
}

var (
	defaultModelName = cmds.Var[string]("-model")
)

type DefaultModelName string

var _ configs.Configurable = DefaultModelName("")

func (d DefaultModelName) TaigoConfigurable() {
}

func (Module) DefaultModelName(
	loader configs.Loader,
	fallback FallbackModelName,
	logger logs.Logger,
) (ret DefaultModelName) {
	defer func() {
		logger.Info("default model", "name", ret)
	}()
	return vars.FirstNonZero(
		DefaultModelName(*defaultModelName),
		configs.First[DefaultModelName](loader, "model_name"),
		configs.First[DefaultModelName](loader, "model"),
		DefaultModelName(fallback),
	)
}

type FallbackModelName string

func (Module) FallbackModelName() FallbackModelName {
	return "gemini-flash"
}
