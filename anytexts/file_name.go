package anytexts

import (
	"regexp"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/vars"
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

var matchFlag = cmds.Var[string]("-match")

type Match string

var _ configs.Configurable = Match("")

func (m Match) TaigoConfigurable() {}

func (Module) Match(
	loader configs.Loader,
) Match {
	return vars.FirstNonZero(
		Match(*matchFlag),
		configs.First[Match](loader, "match"),
	)
}
