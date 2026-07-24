package flags

import (
	"fmt"
	"maps"
)

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

func (m Match) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	// Copy the existing map to preserve scope immutability.
	ret := make(Match, len(m)+1)
	maps.Copy(ret, m)
	ret[args[0]] = true
	newValue = ret
	remainArgs = args[1:]
	return
}
