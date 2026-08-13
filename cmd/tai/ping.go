package main

import (
	"context"
	"os"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/records"
)

const TheoryOfPingCommand = `
The "ping" subcommand tests whether a model is reachable and responding.
It sends a simple "hello" message to the configured model and outputs
the response. This is useful for verifying API keys, network connectivity,
and model availability before starting a generation session. The command
requires a model to be specified via -model; without it, the generator
provider fails during scope resolution, making the dependency on an
explicit model selection explicit. The command performs a single
generation round with no chat loop, no system prompt, and no file context.

Ping runs through the unified generation loop (loops.Run) in single-shot
mode (no components), so it participates in the same mechanisms as the
other generation commands: the TUI's finish-reason observer is applied
via RunOptions.StateDecorators when -tui is enabled, the "generating"
log record drives the TUI's in-flight hint, and interaction recording
is handled by the loop when -record is enabled. See TheoryOfTUI and
loops.TheoryOfLoops.
`

var PingCommand = Command{
	Defs: []any{
		modes.ForProduction(),
	},
	Main: func(
		recorder *records.Recorder,
		generator generators.Generator,
		buildGenerate phases.BuildGenerate,
		loopRun loops.Run,
	) {
		ctx := context.Background()

		var state generators.State
		state = generators.NewPrompts(
			"",
			[]*generators.Content{
				{
					Role: generators.RoleUser,
					Parts: []generators.Part{
						generators.Text("hello"),
					},
				},
			},
		)
		state = generators.NewOutput(state, os.Stdout, true)

		// Run the unified generation loop in single-shot mode (no
		// components). The loop handles ParserState wrapping, phase
		// execution, and interaction recording; the TUI's finish-reason
		// observer is applied via RunOptions.StateDecorators when -tui
		// is enabled. The result is filled into result as the run
		// progresses; the iterator yields the terminal error, if any.
		// See loops.TheoryOfLoops and TheoryOfTUI.
		var result loops.Result
		var err error
		for e := range loopRun(ctx, loops.RunOptions{
			Generator:           generator,
			InitialState:        state,
			Components:          nil,
			Command:             "ping",
			InteractionRecorder: recorder,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return buildGenerate(g, nil)(nil)
			},
		}, &result) {
			err = e
		}
		ce(err)
	},
}
