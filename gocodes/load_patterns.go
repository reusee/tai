package gocodes

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

type LoadPatterns []string

var _ flags.Flag = LoadPatterns{}

var _ configs.Config = LoadPatterns(nil)

func (l LoadPatterns) ConfigPaths() []string {
	return []string{"go.load_patterns", "go.packages", "go.pkgs"}
}

func (l LoadPatterns) HandleConfig(path string, values []*cue.Value) (any, error) {
	var patterns []string
	if err := values[0].Decode(&patterns); err != nil {
		return nil, err
	}
	return LoadPatterns(patterns), nil
}

func (l LoadPatterns) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expected pattern, got empty")
	}
	ret := append(slices.Clone(l), args[0])
	return &ret, args[1:], nil
}

func (l LoadPatterns) Keys() map[string]string {
	return map[string]string{
		"-pkg":  "Add a Go package loading pattern",
		"-load": "Alias for -pkg: add a Go package loading pattern",
	}
}

type ContextPatterns []string

var _ flags.Flag = ContextPatterns{}

var _ configs.Config = ContextPatterns(nil)

func (c ContextPatterns) ConfigPaths() []string {
	return []string{"go.context_patterns"}
}

func (c ContextPatterns) HandleConfig(path string, values []*cue.Value) (any, error) {
	var patterns []string
	if err := values[0].Decode(&patterns); err != nil {
		return nil, err
	}
	return ContextPatterns(patterns), nil
}

func (c ContextPatterns) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expected pattern, got empty")
	}
	ret := append(slices.Clone(c), args[0])
	return &ret, args[1:], nil
}

func (c ContextPatterns) Keys() map[string]string {
	return map[string]string{
		"-ctx": "Add a context package pattern for dependency analysis",
		"-dep": "Alias for -ctx: add a context package pattern",
	}
}

func (Module) LoadArgs() LoadPatterns {
	return LoadPatterns{
		"./...",
	}
}

func (Module) ContextPatterns() ContextPatterns {
	return nil
}
