package main

import (
	"context"
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/taiconfigs"
	"github.com/reusee/tai/taido"
)

var (
	goalFlag = cmds.Var[string]("goal")
)

func main() {
	cmds.Execute(os.Args[1:])

	if *goalFlag == "" {
		fmt.Fprintln(os.Stderr, "Error: goal is required (use 'goal=\"your goal\"')")
		os.Exit(1)
	}

	scope := dscope.New(
		new(taido.Module),
		new(codes.Module),
		modes.ForProduction(),
	).Fork(
		dscope.Provide(codes.CodeProviderName("any")),
		dscope.Provide(codes.DefaultDiffHandlerName("unified")),
	)

	scope, err := taiconfigs.TaigoFork(scope)
	if err != nil {
		panic(err)
	}

	scope.Call(func(
		execute taido.Execute,
		getGenerator generators.GetDefaultGenerator,
		systemPrompt taido.SystemPrompt,
		codeProvider codetypes.CodeProvider,
		diffHandler codetypes.DiffHandler,
		logger logs.Logger,
	) {
		ctx := context.Background()
		generator, err := getGenerator()
		if err != nil {
			panic(err)
		}

		logger.Info("starting autonomous execution",
			"goal", *goalFlag,
			"model", generator.Args().Model,
		)

		// Initial state setup
		var state generators.State
		state = generators.NewPrompts(
			string(systemPrompt),
			[]*generators.Content{
				{
					Role:  generators.RoleUser,
					Parts: []generators.Part{generators.Text(*goalFlag)},
				},
			},
		)
		state = generators.NewOutput(state, os.Stdout, true)
		state = generators.NewFuncMap(state, codeProvider.Functions()...)
		state = generators.NewFuncMap(state, diffHandler.Functions()...)

		// Run the loop
		if err := execute(ctx, generator, state); err != nil {
			fmt.Fprintf(os.Stderr, "Execution failed: %v\n", err)
			os.Exit(1)
		}
	})
}

