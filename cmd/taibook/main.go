package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/prompts"
	"github.com/reusee/tai/taiconfigs"
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
		new(Module),
		modes.ForProduction(),
	)

	scope, err := taiconfigs.TaigoFork(scope)
	ce(err)

	scope.Call(func(
		logger logs.Logger,
		buildGenerate phases.BuildGenerate,
		buildChat phases.BuildChat,
		getGenerator generators.GetDefaultGenerator,
	) {
		ctx := context.Background()

		const playbookFile = "playbook.janet"
		logger.Info("starting playbook execution",
			"goal", *goalFlag,
			"file", playbookFile,
		)

		var state generators.State

		// Try to load existing playbook to maintain "Source as State"
		if content, err := os.ReadFile(playbookFile); err == nil {
			logger.Info("loading existing playbook")
			state = generators.NewPrompts(
				prompts.Playbook,
				[]*generators.Content{
					{
						Role:  generators.RoleUser,
						Parts: []generators.Part{generators.Text("Goal: " + *goalFlag)},
					},
					{
						Role:  generators.RoleAssistant,
						Parts: []generators.Part{generators.Text(string(content))},
					},
					{
						Role:  generators.RoleUser,
						Parts: []generators.Part{generators.Text("Analyze logs and continue execution or optimize.")},
					},
				},
			)
		} else {
			// Initial state setup for a new goal
			state = generators.NewPrompts(
				prompts.Playbook,
				[]*generators.Content{
					{
						Role:  generators.RoleUser,
						Parts: []generators.Part{generators.Text("Goal: " + *goalFlag)},
					},
				},
			)
		}

		// Capture the output to update the playbook file atomically
		outputBuffer := new(bytes.Buffer)
		state = generators.NewOutput(state, io.MultiWriter(os.Stdout, outputBuffer), true)

		generator, err := getGenerator()
		ce(err)

		phase := buildGenerate(generator, nil)(
			buildChat(generator, nil)(
				nil,
			),
		)

		for phase != nil {
			var err error
			phase, state, err = phase(ctx, state)
			ce(err)
		}

		// Save the rewritten playbook source back to the file
		if outputBuffer.Len() > 0 {
			err := os.WriteFile(playbookFile, outputBuffer.Bytes(), 0644)
			ce(err)
			logger.Info("playbook updated")
		}
	})
}

var Theory = `
# Theory of cmd/taibook

The taibook tool is an implementation of the Playbook system, which treats a task's state as a Text-based Virtual Machine (TVM). 

1. Source as State: The entire execution state (variables, program counter, and logs) is persisted in a Janet/Lisp-formatted text file (playbook.janet).
2. Human-AI Symbiosis: Both the AI (The Architect) and humans can read and write to the same file. Edits represent direct state transitions.
3. Execution as Transformation: Progressing a task is equivalent to transforming the playbook source from one state to the next.
4. Strategic Focus: The tool minimizes context bloat by focusing the AI on rewriting the playbook rather than processing infinite chat history.
`

