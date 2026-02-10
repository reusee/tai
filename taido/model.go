package taido

import (
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
)

// TaidoModelName defines the model to be used specifically for autonomous execution.
type TaidoModelName string

func (t TaidoModelName) TaigoConfigurable() {}

var taidoModelFlag = cmds.Var[string]("-taido-model")

// TaidoModelName provider selects the model name based on:
// 1. -taido-model flag
// 2. taido_model config key
// 3. generators.DefaultModelName fallback
func (Module) TaidoModelName(
	loader configs.Loader,
	defaultModel generators.DefaultModelName,
) TaidoModelName {
	var name string
	if taidoModelFlag != nil && *taidoModelFlag != "" {
		name = *taidoModelFlag
	} else {
		loader.AssignFirst("taido_model", &name)
	}
	if name != "" {
		return TaidoModelName(name)
	}
	return TaidoModelName(defaultModel)
}

// GetTaidoGenerator provides a generator instance using the configured TaidoModelName.
type GetTaidoGenerator func() (generators.Generator, error)

func (Module) GetTaidoGenerator(
	get generators.GetGenerator,
	name TaidoModelName,
) GetTaidoGenerator {
	return func() (generators.Generator, error) {
		return get(string(name))
	}
}

