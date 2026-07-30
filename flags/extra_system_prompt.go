package flags

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// ExtraSystemPrompt configs.Config implementation. ExtraSystemPrompt is a
// list of strings that are appended to the system prompt. Values from
// multiple config files and config paths are aggregated additively, not
// replaced. Each config value may be a single string or a list of strings.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = ExtraSystemPrompt(nil)

// ExtraSystemPrompt is a list of additional system prompt sections that are
// appended to the base system prompt. Values from multiple config files and
// config paths are aggregated additively, preserving all contributions rather
// than replacing earlier ones. Each config value may be a single string or a
// list of strings.
type ExtraSystemPrompt []string

func (Module) ExtraSystemPrompt() ExtraSystemPrompt {
	return nil
}

func (e ExtraSystemPrompt) ConfigPaths() []string {
	return []string{"extra_system_prompt"}
}

func (e ExtraSystemPrompt) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := slices.Clone(e)
	for _, v := range values {
		switch v.Kind() {
		case cue.StringKind:
			var s string
			if err := v.Decode(&s); err != nil {
				return nil, err
			}
			if s != "" {
				ret = append(ret, s)
			}
		case cue.ListKind:
			var list []string
			if err := v.Decode(&list); err != nil {
				return nil, err
			}
			ret = append(ret, list...)
		default:
			return nil, fmt.Errorf("expected string or list, got %v", v.Kind())
		}
	}
	return &ret, nil
}
