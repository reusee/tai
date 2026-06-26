package flags

import "github.com/reusee/tai/cmds"

type ModelName string

var modelName ModelName

func init() {
	cmds.Define("-model", cmds.Func(func(name ModelName) {
		modelName = name
	}))
}

func (Module) ModelName() ModelName {
	return modelName
}
