package gocodes

import (
	"fmt"
	"slices"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

type LoadPatterns []string

var _ flags.Flag = LoadPatterns{}

func (l LoadPatterns) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expected pattern, got empty")
	}
	return append(slices.Clone(l), args[0]), args[1:], nil
}

func (l LoadPatterns) Keys() map[string]string {
	return map[string]string{
		"-pkg":  "Add a Go package loading pattern",
		"-load": "Alias for -pkg: add a Go package loading pattern",
	}
}

type ContextPatterns []string

var _ flags.Flag = ContextPatterns{}

func (c ContextPatterns) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expected pattern, got empty")
	}
	return append(slices.Clone(c), args[0]), args[1:], nil
}

func (c ContextPatterns) Keys() map[string]string {
	return map[string]string{
		"-ctx": "Add a context package pattern for dependency analysis",
		"-dep": "Alias for -ctx: add a context package pattern",
	}
}

func (Module) LoadArgs(
	loader configs.Loader,
) LoadPatterns {

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
	return configs.First[ContextPatterns](loader, "go.context_patterns")
}
