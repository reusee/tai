package flags

import "github.com/reusee/tai/cmds"

type Files map[string]bool

var files = make(Files)

func init() {
	cmds.Define("-file", cmds.Func(func(file string) {
		files[file] = true
	}))
}

func (Module) Files() Files {
	return files
}
