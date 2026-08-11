package gocodes

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// ExtraSystemPrompt configs.Config implementation for the gocodes module.
// The go.extra_system_prompt config path provides Go-specific additional
// system prompt sections. Unlike the top-level extra_system_prompt
// (flags.ExtraSystemPrompt), these are only introduced when the go code
// generation pipeline is active: the go and goal commands merge them into
// flags.ExtraSystemPrompt, while other commands (any, ai) ignore them.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = ExtraSystemPrompt(nil)

// ExtraSystemPrompt configs.Config implementation for the gocodes module.
// The go.extra_system_prompt config path provides Go-specific additional
// system prompt sections. codes.CodesComponents injects this type and
// appends each entry as a prompt-only Component, so the prompts are
// introduced whenever the codes generation pipeline is active (go, any,
// goal commands). The ai command uses AIComponents and is unaffected.
// See flags.TheoryOfConfigFlagParity.
type ExtraSystemPrompt []string

func (Module) ExtraSystemPrompt() ExtraSystemPrompt {
	return nil
}

func (e ExtraSystemPrompt) ConfigPaths() []string {
	return []string{"go.extra_system_prompt"}
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
