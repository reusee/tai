package codes

import (
	"cmp"
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
	keys := slices.Collect(maps.Keys(cmdPatterns))
	slices.SortStableFunc(keys, cmp.Compare)
	return keys
}
