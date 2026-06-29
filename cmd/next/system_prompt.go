package main

import (
	_ "embed"
	"maps"
	"slices"
	"strings"

	"github.com/reusee/prompts"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/logs"
)

type ExtraSystemPrompt string

var _ configs.Configurable = ExtraSystemPrompt("")

func (e ExtraSystemPrompt) TaigoConfigurable() {}

func (Module) ExtraSystemPrompt(
	loader configs.Loader,
) ExtraSystemPrompt {
	return configs.First[ExtraSystemPrompt](loader, "extra_system_prompt")
}

type SystemPrompt string

func (Module) SystemPrompt(
	codeProvider anytexts.CodeProvider,
	logger logs.Logger,
	extra ExtraSystemPrompt,
	flagFiles flags.Files,
	flagFocus flags.Focus,
	flagIgnore flags.Ignore,
) (ret SystemPrompt) {

	ret += SystemPrompt(prompts.NextStep)

	patterns := slices.Collect(maps.Keys(flagFiles))

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
		ret += "\n\n" + SystemPrompt((codes.BoundaryDiffHandler{}).SystemPrompt()) + "\n\n"
	}

	if extra != "" {
		ret += "\n\n" + SystemPrompt(extra) + "\n"
	}

	if len(flagFocus) > 0 {
		ret += "\n\n专注于这些方面：\n"
		for _, what := range flagFocus {
			ret += "- " + SystemPrompt(what) + "\n"
		}
	}

	ignore := slices.Collect(maps.Keys(flagIgnore))
	if len(ignore) > 0 {
		ret += "\n\n忽略这些方面：\n"
		for _, what := range ignore {
			ret += "- " + SystemPrompt(what) + "\n"
		}
	}

	return ret
}
