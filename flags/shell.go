package flags

import "github.com/reusee/tai/cmds"

type Shell bool

var shell Shell

func init() {
	cmds.Define("-shell", cmds.Func(func() {
		shell = true
	}))
	cmds.Define("-no-shell", cmds.Func(func() {
		shell = false
	}))
}

func (Module) Shell() Shell {
	return shell
}
