package main

import (
	_ "embed"
	"strings"

	"github.com/reusee/prompts"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
)

type ExtraSystemPrompt string

var _ExtraSystemPromptConfigurable configs.Configurable = ExtraSystemPrompt("")

func (e ExtraSystemPrompt) TaigoConfigurable() {}

func (Module) ExtraSystemPrompt(
	loader configs.Loader,
) ExtraSystemPrompt {
	return configs.First[ExtraSystemPrompt](loader, "extra_system_prompt")
}

type SystemPrompt string

var (
	focus  []string
	ignore []string
)

func init() {
	cmds.Define("-focus", cmds.Func(func(what string) {
		focus = append(focus, what)
	}).Desc("add focus"))
	cmds.Define("-ignore", cmds.Func(func(what string) {
		ignore = append(ignore, what)
	}).Desc("add ignore"))
}

func (Module) SystemPrompt(
	codeProvider anytexts.CodeProvider,
	logger logs.Logger,
	extra ExtraSystemPrompt,
) (ret SystemPrompt) {

	ret += SystemPrompt(prompts.NextStep)

	hasGoFiles := false
	for info, err := range codeProvider.IterFiles(patterns) {
		ce(err)
		if strings.HasSuffix(info.Path, ".go") {
			hasGoFiles = true
			break
		}
	}
	if hasGoFiles {
		logger.Info("has go file")
		ret += "\n\n" + prompts.UnifiedDiff + "\n\n"
	}

	if extra != "" {
		ret += "\n\n" + SystemPrompt(extra) + "\n"
	}

	if len(focus) > 0 {
		ret += "\n\n专注于这些方面：\n"
		for _, what := range focus {
			ret += "- " + SystemPrompt(what) + "\n"
		}
	}

	if len(ignore) > 0 {
		ret += "\n\n忽略这些方面：\n"
		for _, what := range ignore {
			ret += "- " + SystemPrompt(what) + "\n"
		}
	}

	return ret
}
