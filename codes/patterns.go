package codes

import (
	"cmp"
	"maps"
	"slices"

	"github.com/reusee/tai/flags"
)

type Patterns []string

func (Module) Patterns(
	flagFiles flags.Files,
	flagIgnore flags.Ignore,
) Patterns {
	keys := slices.Collect(maps.Keys(flagFiles))
	slices.SortStableFunc(keys, cmp.Compare)
	var ret Patterns
	ret = append(ret, keys...)
	exKeys := slices.Collect(maps.Keys(flagIgnore))
	slices.SortStableFunc(exKeys, cmp.Compare)
	for _, k := range exKeys {
		ret = append(ret, "!"+k)
	}
	return ret
}
