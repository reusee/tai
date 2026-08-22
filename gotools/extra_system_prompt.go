package gotools

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

// FamilyExtraSystemPrompt configs.Config implementation for the gocodes
// module. It maps model family names to Go-specific additional system
// prompt sections. Values from multiple config files and config paths are
// aggregated additively per family. See flags.TheoryOfConfigFlagParity and
// codes.TheoryOfFamilyExtraSystemPrompt.

var _ configs.Config = FamilyExtraSystemPrompt(nil)

// FamilyExtraSystemPrompt is a map from model family names to Go-specific
// additional system prompt sections. The prompts for the resolved
// generator's family are appended after the generic go extra prompts.
// Each config value may be a single string or a list of strings; values
// from multiple config files are aggregated additively per family.
type FamilyExtraSystemPrompt map[string][]string

func (Module) FamilyExtraSystemPrompt() FamilyExtraSystemPrompt {
	return nil
}

func (f FamilyExtraSystemPrompt) ConfigPaths() []string {
	return []string{"go.family_extra_system_prompt"}
}

func (f FamilyExtraSystemPrompt) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := make(FamilyExtraSystemPrompt, len(f))
	for family, prompts := range f {
		ret[family] = slices.Clone(prompts)
	}
	for _, v := range values {
		iter, err := v.Fields()
		if err != nil {
			return nil, err
		}
		for iter.Next() {
			family := iter.Selector().Unquoted()
			val := iter.Value()
			switch val.Kind() {
			case cue.StringKind:
				var s string
				if err := val.Decode(&s); err != nil {
					return nil, err
				}
				if s != "" {
					ret[family] = append(ret[family], s)
				}
			case cue.ListKind:
				var list []string
				if err := val.Decode(&list); err != nil {
					return nil, err
				}
				ret[family] = append(ret[family], list...)
			default:
				return nil, fmt.Errorf("expected string or list for family %q, got %v", family, val.Kind())
			}
		}
	}
	return &ret, nil
}

// ExtraSystemPrompt configs.Config implementation for the gotools module.
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
