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

// FamilyExtraSystemPrompt configs.Config implementation. It maps model
// family names to additional system prompt sections. Values from multiple
// config files and config paths are aggregated additively per family.
// See flags.TheoryOfConfigFlagParity and
// codes.TheoryOfFamilyExtraSystemPrompt.

var _ configs.Config = FamilyExtraSystemPrompt(nil)

// FamilyExtraSystemPrompt is a map from model family names to additional
// system prompt sections. The prompts for the resolved generator's family
// are appended after the generic extra system prompts. Each config value
// may be a single string or a list of strings; values from multiple config
// files are aggregated additively per family.
type FamilyExtraSystemPrompt map[string][]string

func (Module) FamilyExtraSystemPrompt() FamilyExtraSystemPrompt {
	return nil
}

func (f FamilyExtraSystemPrompt) ConfigPaths() []string {
	return []string{"family_extra_system_prompt"}
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
