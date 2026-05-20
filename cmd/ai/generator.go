package main

import (
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
)

func (Module) Generator(
	getDefaultGenerator generators.GetDefaultGenerator,
	getGenerator generators.GetGenerator,
	commandModelName CommandModelName,
) generators.Generator {
	if commandModelName != "" {
		ret, err := getGenerator(string(commandModelName))
		ce(err)
		return ret
	}
	ret, err := getDefaultGenerator()
	ce(err)
	return ret
}

type CommandModelName string

func (Module) CommandModelName(
	loader configs.Loader,
) CommandModelName {
	return configs.First[CommandModelName](loader, "cmd_ai_model")
}
