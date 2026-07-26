package gocodes

import (
	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

// IncludeStdLib configs.Config implementation.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = IncludeStdLib(false)

type IncludeStdLib bool

func (Module) IncludeStdLib() IncludeStdLib {
	return false
}

var _ flags.Flag = IncludeStdLib(false)

func (i IncludeStdLib) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := IncludeStdLib(true)
	return &ret, args, nil
}

func (i IncludeStdLib) Keys() map[string]string {
	return map[string]string{
		"-include-std": "Include standard library packages in the context",
	}
}

func (i IncludeStdLib) ConfigPaths() []string {
	return []string{"go.include_std"}
}

func (i IncludeStdLib) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return IncludeStdLib(b), nil
}

// ShowTokenCounts configs.Config implementation.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = ShowTokenCounts(false)

type ShowTokenCounts bool

func (Module) ShowTokenCounts() ShowTokenCounts {
	return false
}

var _ flags.Flag = ShowTokenCounts(true)

func (s ShowTokenCounts) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := ShowTokenCounts(true)
	return &ret, args, nil
}

func (s ShowTokenCounts) Keys() map[string]string {
	return map[string]string{
		"-show-token-counts": "Display token counts for each included file",
	}
}

func (s ShowTokenCounts) ConfigPaths() []string {
	return []string{"go.show_token_counts"}
}

func (s ShowTokenCounts) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return ShowTokenCounts(b), nil
}
