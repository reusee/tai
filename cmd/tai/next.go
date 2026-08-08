package main

import (
	"context"
	"maps"
	"os"
	"slices"

	"github.com/reusee/prompts"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/records"
)

const TheoryOfNextCommand = `
The "next" subcommand identifies and executes the most valuable next step to
advance the user's goal. It uses the prompts.NextStep system prompt as its
base, augmented with the change block prompt when Go files are
detected in the input, plus optional extra, focus, and ignore directives.
Unlike the "ai" subcommand which supports multi-turn conversation with
memory, shell, and continue blocks, "next" performs a single generation
round: it builds the system prompt and user prompt from file context, runs
one generate-chat phase chain, and writes the result to stdout. This makes
it the simplest entry point for autonomous, single-shot task execution.

Change blocks emitted by the model are applied to the working tree via a
ParserState block handler that writes to an in-memory MemoryStore during
streaming, then flushes to disk after the generation round succeeds. This
reuses the same in-memory apply mechanism as the codes module (see
changes.TheoryOfInMemoryApply), ensuring early error detection — a
malformed change block triggers a retry via changes.ApplyError, resetting
the MemoryStore to discard failed changes — while preserving filesystem
consistency on failure. The handler is built by
changes.BuildChangeBlockHandler, sharing the change-application logic with
the codes module. The -no-apply flag disables change block application,
causing blocks to be parsed but not applied to disk.
`

type SystemPrompt string

func (Module) SystemPrompt(
	logger logs.Logger,
	extra flags.ExtraSystemPrompt,
	hasFiles HasFiles,
	flagFocus flags.Focus,
	flagIgnore flags.Ignore,
) (ret SystemPrompt) {

	ret += SystemPrompt(prompts.NextStep)

	if hasFiles {
		logger.Info("has focus file")
		ret += "\n\n" + SystemPrompt(changes.ChangeBlockSystemPrompt()) + "\n\n"
	}

	for _, e := range extra {
		if e != "" {
			ret += "\n\n" + SystemPrompt(e) + "\n"
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
		flagThoughts flags.Thoughts,
		apply flags.Apply,
		buildChangeBlockHandler changes.BuildChangeBlockHandler,
		loopRun loops.Run,
		recorder *records.Recorder,
		writeTimes *changes.FileWriteTimes,
	) {
		ctx := context.Background()

		// Open a root on the current directory to restrict all file I/O
		// to the project tree during change block application.
		root, err := os.OpenRoot(".")
		ce(err)
		defer root.Close()

		// MemoryStore buffers change block modifications in memory during
		// generation, deferring disk writes until the round succeeds.
		// The underlying root store enables write conflict detection: a
		// file modified externally since the last write is rejected at
		// flush time. See changes.TheoryOfInMemoryApply and
		// changes.TheoryOfWriteConflictDetection.
		memStore := changes.NewMemoryStore(changes.NewRootStoreWithWriteTimes(root, writeTimes))

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
		showThoughts := true
		if flagThoughts.Value != nil {
			showThoughts = *flagThoughts.Value
		}
		state = generators.NewOutput(state, os.Stdout, showThoughts)

		// BlockHandler applies change blocks immediately to the
		// MemoryStore as they are parsed during streaming, enabling
		// early error detection. Apply errors are returned as
		// *changes.ApplyError so the loop can retry, resetting the
		// MemoryStore to discard failed changes. The handler is built
		// by changes.BuildChangeBlockHandler so the change-application
		// logic is shared with the codes module. See
		// changes.TheoryOfInMemoryApply and loops.TheoryOfLoops.
		var blockHandler loops.BlockHandler
		if bool(apply) {
			handler := buildChangeBlockHandler(memStore)
			blockHandler = loops.BlockHandler(handler)
		}

		// Run the unified generation loop in single-shot mode (no
		// components). The phase chain (generate -> chat) drives the
		// interactive session. Apply errors trigger a retry with the
		// error message fed back as user content. The interaction
		// recorder is passed explicitly so the session is captured when
		// -record is enabled. See loops.TheoryOfLoops.
		_, err = loopRun(ctx, loops.RunOptions{
			Generator:           generator,
			InitialState:        state,
			Components:          nil,
			BlockHandler:        blockHandler,
			Command:             "next",
			InteractionRecorder: recorder,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return buildGenerate(g, nil)(buildChat(g, nil)(nil))
			},
			OnRoundStart: func() {
				memStore.Reset()
			},
			RetryOnError: true,
		})
		ce(err)

		// Flush in-memory changes to disk after the generation round
		// succeeds. See changes.TheoryOfInMemoryApply.
		if bool(apply) {
			err = memStore.Flush()
			ce(err)
		}

	},
}
