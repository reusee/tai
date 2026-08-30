package main

import (
	"context"
	"maps"
	"os"
	"slices"

	"github.com/reusee/prompts"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/records"
)

const TheoryOfNextCommand = `
The "next" subcommand identifies and executes the most valuable next step to
advance the user's goal. It uses the prompts.NextStep system prompt as its
base, augmented with the change block prompt when Go files are detected in the
input, plus optional extra, focus, and ignore directives. Unlike the "ai"
subcommand which supports multi-turn conversation with memory, shell, and
continue blocks, "next" performs a single generation: it builds the
system prompt and user prompt from file context, runs one generate-chat phase
chain, and writes the result to stdout. This makes it the simplest entry point
for autonomous, single-shot task execution.

The system prompt carries a disabled-blocks notice
(components.DisabledBlocksNotice) listing shell, continue, go-test, go-src,
and ingest: the single-shot loop runs with no components, so these kinds are
never processed here, and without the notice the model could emit them from
habit and have them silently ignored while implying actions that never
happened. Change is not listed: it is handled by the BlockHandler (or
dry-run under -no-apply) whenever hasFiles included the change prompt. The
notice is static for this command, so it sits directly after the base prompt
inside the stable prefix region. See components.TheoryOfDisabledBlocks.

Change blocks emitted by the model are applied to the working tree via a
ParserState block handler that writes to an in-memory MemoryStore during
streaming, then flushes to disk after the generation succeeds. This
reuses the same in-memory apply mechanism as the pipeline (see
changes.TheoryOfInMemoryApply), ensuring early error detection — a malformed
change block triggers a retry via changes.ApplyError, resetting the
MemoryStore to discard failed changes — while preserving filesystem
consistency on failure. The handler is built by
changes.BuildChangeBlockHandler, sharing the change-application logic with
the pipeline. The -no-apply flag disables change block application,
causing blocks to be parsed but not applied to disk.

The -summarize-thoughts flag wires pipeline.NewThoughtsSummarize around the
output layer, mirroring the generation pipeline: when enabled (and thoughts are
not hidden), the stdout Output layer suppresses raw thoughts and the
summarizer writes periodic summaries to the generation output stream
(os.Stdout), and each summary also flows to the run's event stream as an
EventThoughtSummary, which the TUI renders in its Events tab. In TUI mode the
tuiOutputState decorator still streams raw thoughts to the Output tab. See
pipeline.TheoryOfThoughtsSummarize.

The user prompt places the -chat arguments before the parts provider content
when given, following pipeline.TheoryOfChatBracketing.
`

type SystemPrompt string

func (Module) SystemPrompt(
	logger logs.Logger,
	extra flags.ExtraSystemPrompt,
	familyExtra flags.FamilyExtraSystemPrompt,
	modelFamily generators.ModelFamily,
	hasFiles HasFiles,
	flagFocus flags.Focus,
	flagIgnore flags.Ignore,
) (ret SystemPrompt) {

	ret += SystemPrompt(prompts.NextStep)

	// Disabled-blocks notice: the next command runs a single-shot loop
	// with no components, so the component-driven kinds are never
	// processed here — shell commands are not run, no next round is
	// triggered by a continue block, and no context, symbol sources, or
	// test results are fetched. Listing them explicitly prevents blocks
	// that would be silently ignored while implying actions that never
	// happened. Change is not listed: it is handled by the BlockHandler
	// (or dry-run under -no-apply) whenever hasFiles included the change
	// prompt. The notice is static for this command, so it sits directly
	// after the base prompt inside the stable prefix region. See
	// components.TheoryOfDisabledBlocks and TheoryOfNextCommand.
	ret += "\n\n" + SystemPrompt(components.DisabledBlocksNotice(
		"shell", "continue", "go-test", "go-src", "ingest",
	))

	if hasFiles {
		logger.Info("has focus file")
		ret += "\n\n" + SystemPrompt(changes.ChangeBlockSystemPrompt()) + "\n\n"
	}

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
	"Identify and execute the most valuable next step",
	func(
		getDefaultGenerator generators.GetDefaultGenerator,
		systemPrompt SystemPrompt,
		userPrompt UserPrompt,
		logger logs.Logger,
		buildGenerate generators.BuildGenerate,
		buildChat pipeline.BuildChat,
		flagThoughts flags.Thoughts,
		apply flags.Apply,
		buildChangeBlockHandler changes.BuildChangeBlockHandler,
		loopRun pipeline.Run,
		recorder *records.Recorder,
		writeTimes *changes.FileWriteTimes,
		getDefaultSummarizer pipeline.GetDefaultSummarizer,
		summarizeThoughts flags.SummarizeThoughts,
	) {
		ctx := context.Background()

		generator, err := getDefaultGenerator()
		ce(err)

		root, err := os.OpenRoot(".")
		ce(err)
		defer root.Close()

		memStore := changes.NewMemoryStore(changes.NewRootStoreWithWriteTimes(root, writeTimes))

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

		// BlockHandler applies change blocks immediately to the
		// MemoryStore as they are parsed during streaming, enabling
		// early error detection. Apply errors are returned as
		// *changes.ApplyError so the loop can retry, resetting the
		// MemoryStore to discard failed changes. The handler is built
		// by changes.BuildChangeBlockHandler so the change-application
		// logic is shared with the pipeline. See
		// changes.TheoryOfInMemoryApply and pipeline.TheoryOfLoops.
		var blockHandler pipeline.BlockHandler
		if bool(apply) {
			handler := buildChangeBlockHandler(memStore)
			blockHandler = pipeline.BlockHandler(handler)
		}

		// Run the unified generation loop in single-shot mode (no
		// components). The phase chain (generate -> chat) drives the
		// interactive session. Apply errors trigger a retry with the
		// error message fed back as user content. The interaction
		// recorder is passed explicitly so the session is captured when
		// -record is enabled. The result is filled into result as the run
		// progresses; the iterator yields the run's events, and the
		// terminal error, if any, arrives with the final yield's error
		// component. See pipeline.TheoryOfLoops and
		// pipeline.TheoryOfLoopEvents.
		var result pipeline.Result
		for _, e := range loopRun(ctx, pipeline.RunOptions{
			Generator:           generator,
			InitialState:        state,
			Components:          nil,
			BlockHandler:        blockHandler,
			Command:             "next",
			InteractionRecorder: recorder,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return buildGenerate(g, nil)(buildChat(g, nil)(nil))
			},
			OnAttemptStart: func() {
				memStore.Reset()
			},
			RetryOnError: true,
		}, &result) {
			if e != nil {
				err = e
			}
		}
		ce(err)

		if bool(apply) {
			err = memStore.Flush()
			ce(err)
		}

	},
	modes.ForProduction(),
	new(apps.Interactive(true)),
)
