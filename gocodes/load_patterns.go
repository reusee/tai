package gocodes

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

const (
	// TheoryOfDefaultLoadPattern explains why an explicit -pkg flag
	// replaces the default ./... pattern instead of appending to it.
	TheoryOfDefaultLoadPattern = `
The default focus load pattern is ./..., loading every package in the
current directory as a focus package. An explicit -pkg flag replaces this
default instead of appending to it: appending keeps ./... in the pattern
list, so every package in the current directory remains a focus package
and -pkg cannot limit the focus to the specified packages. Replacement is
detected by comparing the current scope value against the exact default
[./...]; such a value is indistinguishable from the default regardless of
origin (module provider or config file), so it is always replaced. After
the first explicit pattern, later -pkg flags accumulate. To combine the
default with additional packages, pass -pkg ./... explicitly.
`

	// DefaultLoadPattern is the pattern used to load focus packages when
	// no -pkg flag is given: every package in the current directory. An
	// explicit -pkg flag replaces this default rather than appending to
	// it, so focus is limited to the specified packages. See
	// TheoryOfDefaultLoadPattern.
	DefaultLoadPattern = "./..."
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
	ret := LoadPatterns(patterns)
	return &ret, nil
}

func (l LoadPatterns) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expected pattern, got empty")
	}
	ret := slices.Clone(l)
	// An explicit -pkg flag replaces the default ./... pattern so that
	// focus is limited to the specified packages; appending would keep
	// ./... and load every package in the current directory as a focus
	// package. See TheoryOfDefaultLoadPattern.
	if len(ret) == 1 && ret[0] == DefaultLoadPattern {
		ret = nil
	}
	ret = append(ret, args[0])
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
	ret := ContextPatterns(patterns)
	return &ret, nil
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
		DefaultLoadPattern,
	}
}

func (Module) ContextPatterns() ContextPatterns {
	return nil
}
