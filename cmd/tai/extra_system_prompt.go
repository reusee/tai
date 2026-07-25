package main

import "github.com/reusee/tai/configs"

type ExtraSystemPrompt string

func (Module) ExtraSystemPrompt(
	loader configs.Loader,
) ExtraSystemPrompt {
	return configs.First[ExtraSystemPrompt](loader, "extra_system_prompt")
}
