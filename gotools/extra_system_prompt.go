package gotools

import (
	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// FamilyExtraSystemPrompt configs.Config implementation for the gotools
// module. It maps model family names to Go-specific additional system
// prompt sections. Values from multiple config files and config paths are
// aggregated additively per family. See flags.TheoryOfConfigFlagParity and
// pipeline.TheoryOfFamilyExtraSystemPrompt.

var _ configs.Config = FamilyExtraSystemPrompt(nil)

// FamilyExtraSystemPrompt is a map from model family names to Go-specific
// additional system prompt sections. The prompts for the resolved
// generator's family are appended after the generic go extra prompts.
// Each config value may be a single string or a list of strings; values
// from multiple config files are aggregated additively per family.
// pipeline.CodesComponents injects them only in Go sessions — when the
// session's parts provider is gotools.PartsProvider — so the non-Go
// any_text command never carries them. See
// pipeline.TheoryOfFamilyExtraSystemPrompt.
type FamilyExtraSystemPrompt map[string][]string

func (Module) FamilyExtraSystemPrompt() FamilyExtraSystemPrompt {
	return nil
}

func (f FamilyExtraSystemPrompt) ConfigPaths() []string {
	return []string{"go.family_extra_system_prompt"}
}

func (f FamilyExtraSystemPrompt) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret, err := configs.AppendFamilyStringsConfig(f, values)
	if err != nil {
		return nil, err
	}
	v := FamilyExtraSystemPrompt(ret)
	return &v, nil
}

// ExtraSystemPrompt configs.Config implementation for the gotools module.
// The go.extra_system_prompt config path provides Go-specific additional
// system prompt sections. pipeline.CodesComponents injects this type and
// appends each entry as a prompt-only Component only in Go sessions — when
// the session's parts provider is gotools.PartsProvider — so the prompts
// reach the go_module default command and never the non-Go any_text
// command. The ai command uses AIComponents and is unaffected. See
// flags.TheoryOfConfigFlagParity.
type ExtraSystemPrompt []string

func (Module) ExtraSystemPrompt() ExtraSystemPrompt {
	return nil
}

func (e ExtraSystemPrompt) ConfigPaths() []string {
	return []string{"go.extra_system_prompt"}
}

func (e ExtraSystemPrompt) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret, err := configs.AppendStringsConfig(e, values)
	if err != nil {
		return nil, err
	}
	v := ExtraSystemPrompt(ret)
	return &v, nil
}
