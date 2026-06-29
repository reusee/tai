package flags

import "github.com/reusee/tai/cmds"

type Ignore map[string]bool

var ignore Ignore = make(Ignore)

func init() {
	cmds.Define("-ignore", cmds.Func(func(what string) {
		ignore[what] = true
	}).Alias("-skip", "-exclude"))
}

func (Module) Ignore() Ignore {
	return ignore
}
