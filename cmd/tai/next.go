package main

import (
	"context"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/reusee/prompts"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
)

const TheoryOfNextCommand = `
The "next" subcommand identifies and executes the most valuable next step to
advance the user's goal. It uses the prompts.NextStep system prompt as its
base, augmented with the boundary diff handler prompt when Go files are
detected in the input, plus optional extra, focus, and ignore directives.
Unlike the "ai" subcommand which supports multi-turn conversation with
memory, shell, and continue blocks, "next" performs a single generation
round: it builds the system prompt and user prompt from file context, runs
one generate-chat phase chain, and writes the result to stdout. This makes
it the simplest entry point for autonomous, single-shot task execution.
`

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

var NextCommand = Command{
	Defs: []any{
		modes.ForProduction(),
	},
	Main: func(
		generator generators.Generator,
		systemPrompt SystemPrompt,
		userPrompt UserPrompt,
		logger logs.Logger,
		buildGenerate phases.BuildGenerate,
		buildChat phases.BuildChat,
	) {
		ctx := context.Background()

		// generate
		logger.Info("generate", "model", generator.Spec().Model)
		var state generators.State
		state = generators.NewPrompts(
			string(systemPrompt),
			[]*generators.Content{
				{
					Role:  "user",
					Parts: userPrompt,
				},
			},
		)
		state = generators.NewOutput(state, os.Stdout, true)

		phase := buildGenerate(generator, nil)(
			buildChat(generator, nil)(
				nil,
			),
		)
		var err error
		for phase != nil {
			phase, state, err = phase(ctx, state)
			ce(err)
		}

	},
}
