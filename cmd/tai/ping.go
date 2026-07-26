package main

import (
	"context"
	"os"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
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
`

var PingCommand = Command{
	Defs: []any{
		modes.ForProduction(),
	},
	Main: func(
		generator generators.Generator,
		buildGenerate phases.BuildGenerate,
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

		phase := buildGenerate(generator, nil)(nil)
		for phase != nil {
			var err error
			phase, state, err = phase(ctx, state)
			ce(err)
		}
	},
}
