package flags

import "github.com/reusee/tai/cmds"

type Thoughts *bool

var thoughts Thoughts

func init() {
	cmds.Define("-thoughts", cmds.Func(func() {
		thoughts = new(true)
	}))
	cmds.Define("-no-thoughts", cmds.Func(func() {
		thoughts = new(false)
	}))
}

func (Module) Thoughts() Thoughts {
	return thoughts
}
