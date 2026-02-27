package main

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/prompts"
	"github.com/reusee/tai/taiconfigs"
	"github.com/reusee/tai/taiplay"
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

	scope.Call(runArchitect)
}

func runArchitect(
	logger logs.Logger,
	buildGenerate phases.BuildGenerate,
	getGenerator generators.GetDefaultGenerator,
) {
	ctx := context.Background()

	const playbookFile = "tai.playbook"
	logger.Info("starting playbook execution",
		"goal", *goalFlag,
		"file", playbookFile,
	)

	state := initState(logger, playbookFile)

	// Dual output stream setup:
	// 1. Terminal output includes AI thoughts for transparency.
	// 2. Playbook buffer excludes thoughts to ensure a clean, parsable Lisp file.
	state = generators.NewOutput(state, os.Stdout, true)
	playbookBuffer := new(bytes.Buffer)
	state = generators.NewOutput(state, playbookBuffer, false)

	generator, err := getGenerator()
	ce(err)

	// Create the phase chain. We perform a single generation pass to update the playbook.
	// There is no interactive chat phase, as the "Source as State" philosophy
	// prioritizes the playbook as the authoritative communication medium.
	phase := buildGenerate(generator, nil)(nil)

	for phase != nil {
		var err error
		phase, state, err = phase(ctx, state)
		ce(err)
	}

	// Ensure all content is flushed
	state, err = state.Flush()
	ce(err)

	applyPatches(logger, playbookBuffer)
}

func initState(logger logs.Logger, playbookFile string) generators.State {
	// Try to load existing playbook to maintain "Source as State"
	if content, err := os.ReadFile(playbookFile); err == nil {
		logger.Info("loading existing playbook")
		return generators.NewPrompts(
			prompts.Playbook,
			[]*generators.Content{
				{
					Role:  generators.RoleUser,
					Parts: []generators.Part{generators.Text("Goal: " + *goalFlag)},
				},
				{
					Role: generators.RoleUser,
					// Represent the playbook as the current objective state of the environment,
					// not as a previous turn in the conversation.
					Parts: []generators.Part{generators.Text("Current Playbook Content:\n" + string(content))},
				},
				{
					Role: generators.RoleUser,
					// Align the prompt with the "Optimization" philosophy.
					// We instruct the AI to optimize or align, and explicitly allow no changes if already optimal.
					// We also explicitly mention the execution log constraint.
					Parts: []generators.Part{generators.Text(
						"Analyze the current playbook and execution logs. " +
							"1. If the playbook already has a complete plan for the goal and there are no execution logs yet, " +
							"this means the engine hasn't run it; do not add more steps or logs. " +
							"2. If logs indicate progress, continue the plan. " +
							"3. If logs indicate failure, patch the state to recover. " +
							"4. If the goal is fully achieved according to logs, stop. " +
							"If no changes are necessary to reach the goal or the current state is optimal, do not provide any patches.",
					)},
				},
			},
		)
	}

	// Initial state setup for a new goal
	return generators.NewPrompts(
		prompts.Playbook,
		[]*generators.Content{
			{
				Role: generators.RoleUser,
				Parts: []generators.Part{generators.Text(
					"Goal: " + *goalFlag + "\n\n" +
						"Initialize the playbook using S-expression patches (e.g., S-ADD_BEFORE BEGIN).",
				)},
			},
		},
	)
}

func applyPatches(logger logs.Logger, playbookBuffer *bytes.Buffer) {
	// Save the rewritten playbook source back to the file.
	// We strictly enforce patches for efficiency and safety.
	// Direct text output that is not a patch is ignored to prevent corruption.
	if playbookBuffer.Len() > 0 {
		root, err := os.OpenRoot(".")
		ce(err)
		applied, err := taiplay.ApplySexprPatches(root, playbookBuffer.Bytes())
		ce(err)
		if applied {
			logger.Info("playbook updated via patches")
		} else {
			// If no patches are found, it might be an intentional decision by the AI not to optimize.
			// We log this as information rather than a warning to reflect the optimization philosophy.
			logger.Info("no valid patches found in output; playbook file remains unchanged")
		}
	} else {
		logger.Warn("no playbook output produced during this run")
	}
}

var Theory = `
# Theory of cmd/taiplay

The taiplay tool is an implementation of the Playbook system, which treats a task's state as a Text-based Virtual Machine (TVM). 

1. Source as State: The entire execution state (variables, program counter, and logs) is persisted in a Lisp-formatted text file (tai.playbook). This file is the authoritative, human-readable record of the "Theory of the Task."
2. Human-AI Symbiosis: Both the AI (The Architect) and humans can read and write to the same file. Edits represent direct state transitions in the virtual machine.
3. Execution as Transformation: Progressing a task is equivalent to transforming the playbook source from one state to the next (Term Rewriting).
4. Strategic Focus: The tool minimizes context bloat by focusing the AI on rewriting the playbook rather than processing infinite, unstructured chat history.
5. Strict Patch Enforcement: S-expression based patches (S-MODIFY, S-DELETE, etc.) are the *only* allowed mechanism for updating the playbook file. This ensures that only well-structured transitions are applied, preventing natural language explanations or other non-Lisp content from corrupting the program state.
6. Dual Output Streams: We distinguish between "Human Interface" (Terminal) and "System State" (File). The terminal stream includes the Architect's reasoning (thoughts), while the file stream suppresses them to maintain syntactic purity of the Playbook.
7. Atomic Pass: The command performs a single generation pass to update the state. No interactive dialogue is provided within the tool; all "conversation" occurs through edits to the playbook file itself.
8. No Execution Responsibility: taiplay is strictly an architecting and state-transition tool. It does not execute the instructions (e.g., shell commands, scripts) defined in the playbook. Execution is handled by a separate, specialized engine. The Architect must never simulate or hallucinate results for instructions it creates or modifies.
9. Goal Alignment and Optimization: When a playbook already exists, the task is to align the program with the user's intent or optimize it based on execution logs. Optimization prioritizes efficiency and goal attainment. If the existing state is already optimal, the Architect should choose not to edit the file.
10. Execution-Driven Evolution (Reactive Planning): The Architect should only modify a complete plan if new execution logs are present. Without logs, no progress can be objectively determined, and adding redundant steps leads to "State Inflation."
`

var ObsoleteTheory = `
# Obsolete: Assistant-Role Playbook Representation (Feb 2026)
Initially, the existing playbook content was presented to the AI as a previous turn in the conversation (Assistant role). This caused the "Diligent Architect" issue: the AI perceived its own previous output and felt compelled to continue or refine it every time taiplay was run, leading to redundant appends (e.g., S-ADD_AFTER END) even when no execution had occurred. 

The representation was changed to a User-role description of the "Current Playbook Content" to frame the file as an objective environment state, successfully decoupling the AI's urge to chat from its duty to architect.
`