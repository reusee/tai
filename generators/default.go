package generators

import (
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
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

type DefaultModelName string

func (Module) DefaultModelName(
	loader configs.Loader,
	fallback FallbackModelName,
	logger logs.Logger,
	appName apps.Name,
	flagModelName flags.ModelName,
) (ret DefaultModelName) {
	defer func() {
		logger.Info("default model", "name", ret)
	}()
	return vars.FirstNonZero(
		DefaultModelName(flagModelName),
		configs.First[DefaultModelName](loader, string(appName)+".model_name"),
		configs.First[DefaultModelName](loader, string(appName)+".model"),
		configs.First[DefaultModelName](loader, "model_name"),
		configs.First[DefaultModelName](loader, "model"),
		DefaultModelName(fallback),
	)
}

type FallbackModelName string

func (Module) FallbackModelName() FallbackModelName {
	return "gemini-flash"
}

type GetDefaultFastModel func() (Generator, error)

func (Module) GetDefaultFastModel(
	name DefaultFastModelName,
	get GetGenerator,
) GetDefaultFastModel {
	return func() (Generator, error) {
		return get(string(name))
	}
}

type DefaultFastModelName string

func (Module) DefaultFastModelName(
	loader configs.Loader,
	fallback FallbackFastModelName,
	logger logs.Logger,
	appName apps.Name,
	flagFastModelName flags.FastModelName,
) (ret DefaultFastModelName) {
	defer func() {
		logger.Info("default fast model", "name", ret)
	}()
	return vars.FirstNonZero(
		DefaultFastModelName(flagFastModelName),
		configs.First[DefaultFastModelName](loader, string(appName)+".fast_model_name"),
		configs.First[DefaultFastModelName](loader, string(appName)+".fast_model"),
		configs.First[DefaultFastModelName](loader, "fast_model_name"),
		configs.First[DefaultFastModelName](loader, "fast_model"),
		DefaultFastModelName(fallback),
	)
}

type FallbackFastModelName string

func (Module) FallbackFastModelName() FallbackFastModelName {
	return "gemini-flash"
}
