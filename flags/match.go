package flags

import "github.com/reusee/tai/cmds"

type Match map[string]bool

var match Match = make(Match)

func init() {
	cmds.Define("-match", cmds.Func(func(what string) {
		match[what] = true
	}).Alias("-include"))
}

func (Module) Match() Match {
	return match
}
