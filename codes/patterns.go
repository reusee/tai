package codes

import (
	"cmp"
	"maps"
	"slices"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/flags"
)

type Patterns []string

var cmdExcludePatterns = map[string]bool{}

func init() {
	cmds.Define("-exclude", cmds.Func(func(pattern string) {
		cmdExcludePatterns[pattern] = true
	}))
	cmds.Define("-skip", cmds.Func(func(pattern string) {
		cmdExcludePatterns[pattern] = true
	}))
}

func (Module) Patterns(
	flagFiles flags.Files,
) Patterns {
	keys := slices.Collect(maps.Keys(flagFiles))
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

