package anytexts

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

type FileNameOK func(name string) bool

func (Module) FileNameOK() FileNameOK {
	return func(name string) bool {
		return true
	}
}

type NameMatch func(string) bool

func (Module) NameMatch(
	match Match,
) NameMatch {
	if match == "" {
		return func(string) bool {
			return true
		}
	}
	re := regexp.MustCompile(string(match))
	return func(path string) bool {
		return re.MatchString(path)
	}
}

type Match string

var _ configs.Configurable = Match("")

func (m Match) TaigoConfigurable() {}

func (Module) Match(
	loader configs.Loader,
	flagMatch flags.Match,
) Match {

	patterns := slices.Collect(maps.Keys(flagMatch))
	if len(patterns) > 0 {
		for i := range patterns {
			patterns[i] = "(" + patterns[i] + ")"
		}
		return Match(strings.Join(patterns, "|"))
	}

	if pattern := configs.First[Match](loader, "match"); pattern != "" {
		return pattern
	}

	return ""
}
