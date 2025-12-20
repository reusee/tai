package codes

import (
	"maps"
	"slices"

	"github.com/reusee/tai/cmds"
)

type Patterns []string

var cmdPatterns = map[string]bool{}

func init() {
	cmds.Define("-file", cmds.Func(func(pattern string) {
		cmdPatterns[pattern] = true
	}))
}

func (Module) Patterns() Patterns {
	return slices.Collect(maps.Keys(cmdPatterns))
}
