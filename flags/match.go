package flags

import (
	"fmt"
	"maps"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// Match configs.Config implementation. The "match_patterns" path
// provides a list of regex patterns; the legacy "match" path provides
// a single regex string. Both are merged additively with the existing
// flag-accumulated value. See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Match(nil)

type Match map[string]bool

func (Module) Match() (ret Match) {
	return
}

var _ Flag = Match(nil)

func (m Match) Keys() map[string]string {
	return map[string]string{
		"-match":   "Match files by regex pattern for inclusion",
		"-include": "Alias for -match: match files by regex pattern for inclusion",
	}
}

func (m Match) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	// Copy the existing map to preserve scope immutability.
	ret := make(Match, len(m)+1)
	maps.Copy(ret, m)
	ret[args[0]] = true
	return &ret, args[1:], nil
}

func (m Match) ConfigPaths() []string {
	return []string{"match_patterns", "match"}
}

func (m Match) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := make(Match, len(m))
	maps.Copy(ret, m)
	for _, v := range values {
		if path == "match" {
			var s string
			if err := v.Decode(&s); err != nil {
				return nil, err
			}
			ret[s] = true
		} else {
			var patterns []string
			if err := v.Decode(&patterns); err != nil {
				return nil, err
			}
			for _, p := range patterns {
				ret[p] = true
			}
		}
	}
	return ret, nil
}
