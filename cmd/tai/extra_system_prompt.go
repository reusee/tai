package main

import (
	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

type ExtraSystemPrompt string

var _ configs.Config = ExtraSystemPrompt("")

func (e ExtraSystemPrompt) ConfigPaths() []string {
	return []string{"extra_system_prompt"}
}

func (e ExtraSystemPrompt) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return ExtraSystemPrompt(s), nil
}

func (Module) ExtraSystemPrompt() ExtraSystemPrompt {
	return ExtraSystemPrompt("")
}
