package main

import (
	"context"
	"maps"
	"os"
	"slices"

	"github.com/reusee/prompts"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
)

const TheoryOfNextCommand = `
The "next" subcommand identifies the most valuable next step to advance
the user's goal. It is a text-output command: it uses the prompts.NextStep
system prompt as its base, augmented with optional extra, focus, and
ignore directives. In the staged system (see
pipeline.TheoryOfContextPhilosophy) it is the no-component command: the
generate-chat chain serves one generation per user input from the context
assembled upfront, and no block kind opens a further round. The staged
commands — the auto-detected default's goal loops and the ai command's
component rounds — fetch context and generate over multiple rounds;
"next" answers from the context it was given.

The next command never modifies files and processes no block kind: the
system prompt carries a disabled-blocks notice
(components.DisabledBlocksNotice) listing shell, continue, change,
go-test, go-src, and ingest. The loop runs with no components and no
change-block handler, so these kinds are never processed here; without the
notice the model could emit them from habit and have them silently ignored
while implying actions that never happened. The notice is static for this
command, so it sits directly after the base prompt inside the stable
prefix region. See components.TheoryOfDisabledBlocks.

The -summarize-thoughts flag wires pipeline.NewThoughtsSummarize around the
output layer, mirroring the ai command (see pipeline.TheoryOfThoughtsSummarize).

The user prompt places the chat arguments before the parts provider content
when given, following pipeline.TheoryOfChatBracketing.
`

type SystemPrompt string

func (Module) SystemPrompt(
	extra flags.ExtraSystemPrompt,
	familyExtra flags.FamilyExtraSystemPrompt,
	modelFamily generators.ModelFamily,
	flagFocus flags.Focus,
	flagIgnore flags.Ignore,
) (ret SystemPrompt) {

	ret += SystemPrompt(prompts.NextStep)

	// Disabled-blocks notice: the next command is a text-output command.
	// It runs the loop with no components and no change-block handler,
	// so no block kind is processed here — shell commands are not run,
	// no next round is triggered, nothing is fetched, and no change is
	// applied to any file. Listing them explicitly prevents blocks that
	// would be silently ignored while implying actions that never
	// happened. The notice is static for this command, so it sits
	// directly after the base prompt inside the stable prefix region.
	// See components.TheoryOfDisabledBlocks and TheoryOfNextCommand.
	ret += "\n\n" + SystemPrompt(components.DisabledBlocksNotice(
		"shell", "continue", "change", "go-test", "go-src", "ingest",
	))

	for _, e := range extra {
		if e != "" {
			ret += "\n\n" + SystemPrompt(e) + "\n"
		}
	}

	// Family-specific extra system prompts: top-level prompts keyed by
	// the model family. The family is resolved from the scope via
	// generators.ModelFamily; when the family matches a key, the
	// corresponding prompts are appended after the generic extra prompts.
	// See pipeline.TheoryOfFamilyExtraSystemPrompt.
	for _, prompt := range familyExtra[string(modelFamily)] {
		if prompt != "" {
			ret += "\n\n" + SystemPrompt(prompt) + "\n"
		}
	}

	if len(flagFocus) > 0 {
		ret += "\n\n专注于这些方面：\n"
		for _, what := range flagFocus {
			ret += "- " + SystemPrompt(what) + "\n"
		}
	}

	// Ignore items are sorted for prompt determinism: the ignore set is
	// stored in a map, and maps.Keys iteration order is non-deterministic.
	// Without sorting, the ignore section of the system prompt would differ
	// byte-wise across runs with equal configuration, invalidating the LLM
	// prefix cache from the first ignore line onward. Focus items keep
	// their user-specified order because they come from a list. See
	// TheoryOfPrefixCaching in generators/state_func_map.go.
	ignore := slices.Collect(maps.Keys(flagIgnore))
	slices.Sort(ignore)
	if len(ignore) > 0 {
		ret += "\n\n忽略这些方面：\n"
		for _, what := range ignore {
			ret += "- " + SystemPrompt(what) + "\n"
		}
	}

	return
}

var NextCommand = apps.New("next",
	"Identify the most valuable next step",
	func(
		getDefaultGenerator generators.GetDefaultGenerator,
		systemPrompt SystemPrompt,
		userPrompt UserPrompt,
		logger logs.Logger,
		buildGenerate generators.BuildGenerate,
		buildChat pipeline.BuildChat,
		flagThoughts flags.Thoughts,
		loopRun pipeline.Run,
		getDefaultSummarizer pipeline.GetDefaultSummarizer,
		summarizeThoughts flags.SummarizeThoughts,
	) {
		ctx := context.Background()

		generator, err := getDefaultGenerator()
		ce(err)

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
		showThoughts := true
		if flagThoughts.Value != nil {
			showThoughts = *flagThoughts.Value
		}

		if showThoughts && bool(summarizeThoughts) {
			summarizer, err := getDefaultSummarizer()
			ce(err)
			state = generators.NewOutput(state, os.Stdout, false)
			state = pipeline.NewThoughtsSummarize(ctx, state, summarizer, os.Stdout)
		} else {
			state = generators.NewOutput(state, os.Stdout, showThoughts)
		}

		// Run the unified generation loop with no components and no
		// change-block handler: next is a text-output command, so
		// collected blocks are never applied to the working tree. The
		// phase chain (generate -> chat) drives the interactive
		// session. Generation errors after content output
		// retry with the error message fed back as user content. The
		// loop's recording session is opened through the scope's
		// recorder, so the session is captured when -record is enabled
		// without the command carrying the recorder itself. The result
		// is filled into result as the run progresses; every tree yield
		// carries the run's full session tree — the loop's own event
		// nodes included — and the terminal error, if any, arrives with
		// the final yield's error component. See pipeline.TheoryOfLoops,
		// pipeline.TheoryOfLoopEvents and
		// records.TheoryOfInteractionRecording.
		var result pipeline.Result
		for _, e := range loopRun(ctx, pipeline.RunOptions{
			Generator:    generator,
			InitialState: state,
			Components:   nil,
			Command:      "next",
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return buildGenerate(g, nil)(buildChat(g, nil)(nil))
			},
			RetryOnError: true,
		}, &result) {
			if e != nil {
				err = e
			}
		}
		ce(err)

	},
	modes.ForProduction(),
	new(apps.Interactive(true)),
)
