package flags

import "github.com/reusee/tai/cmds"

type Focus []string

var focus Focus

func init() {
	cmds.Define("-focus", cmds.Func(func(what string) {
		focus = append(focus, what)
	}))
}

func (Module) Focus() Focus {
	return focus
}
