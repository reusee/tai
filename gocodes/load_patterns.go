package gocodes

import (
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
)

type LoadPatterns []string

type ContextPatterns []string

var (
	loadPatternsFlag    []string
	contextPatternsFlag []string
)

func init() {
	cmds.Define("-load", cmds.Func(func(pattern string) {
		loadPatternsFlag = append(loadPatternsFlag, pattern)
	}).Alias("-pkg"))
	cmds.Define("-ctx", cmds.Func(func(pattern string) {
		contextPatternsFlag = append(contextPatternsFlag, pattern)
	}).Alias("-dep"))
}

func (Module) LoadArgs(
	loader configs.Loader,
) LoadPatterns {

	if len(loadPatternsFlag) > 0 {
		return LoadPatterns(loadPatternsFlag)
	}

	for _, path := range []string{
		"go.load_patterns",
		"go.packages",
		"go.pkgs",
	} {
		if args := configs.First[LoadPatterns](loader, path); len(args) > 0 {
			return args
		}
	}

	return LoadPatterns{
		"./...",
	}
}

func (Module) ContextPatterns(
	loader configs.Loader,
) ContextPatterns {
	if len(contextPatternsFlag) > 0 {
		return ContextPatterns(contextPatternsFlag)
	}
	return configs.First[ContextPatterns](loader, "go.context_patterns")
}
