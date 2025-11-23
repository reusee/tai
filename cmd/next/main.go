package main

import (
	"context"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
)

func main() {
	cmds.Execute(os.Args[1:])

	dscope.New(
		new(Module),
		modes.ForProduction(),
	).Call(func(
		generator Generator,
		systemPrompt SystemPrompt,
		userPrompt UserPrompt,
		logger logs.Logger,
		buildGenerate phases.BuildGenerate,
		buildChat phases.BuildChat,
	) {
		ctx := context.Background()

		// generate
		logger.Info("generate", "model", generator.Args().Model)
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

		phase := buildGenerate(
			generator,
			buildChat(generator, nil),
		)
		var err error
		for phase != nil {
			phase, state, err = phase(ctx, state)
			ce(err)
		}

	})
}
