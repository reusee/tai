package flags

import "github.com/reusee/tai/cmds"

type Effort string

var effort Effort

func init() {
	cmds.Define("-effort", cmds.Func(func(level Effort) {
		effort = level
	}))
}

func (Module) Effort() Effort {
	return effort
}
