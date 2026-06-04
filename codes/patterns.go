package codes

import (
	"cmp"
	"maps"
	"slices"

	"github.com/reusee/tai/cmds"
)

type Patterns []string

var cmdPatterns = map[string]bool{}

var cmdExcludePatterns = map[string]bool{}

func init() {
	cmds.Define("-file", cmds.Func(func(pattern string) {
		cmdPatterns[pattern] = true
	}))
	cmds.Define("-exclude", cmds.Func(func(pattern string) {
		cmdExcludePatterns[pattern] = true
	}))
	cmds.Define("-skip", cmds.Func(func(pattern string) {
		cmdExcludePatterns[pattern] = true
	}))
}

func (Module) Patterns() Patterns {
	keys := slices.Collect(maps.Keys(cmdPatterns))
	slices.SortStableFunc(keys, cmp.Compare)
	var ret Patterns
	ret = append(ret, keys...)
	exKeys := slices.Collect(maps.Keys(cmdExcludePatterns))
	slices.SortStableFunc(exKeys, cmp.Compare)
	for _, k := range exKeys {
		ret = append(ret, "!"+k)
	}
	return ret
}