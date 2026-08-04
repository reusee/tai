package main

import (
	"context"
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/prompts"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/gocodes"
	"github.com/reusee/tai/modes"
)

const TheoryOfGoalCommand = `
The goal command autonomously runs the generation pipeline for a set number of
iterations (maxGoalIterations) until a done block is observed. Each loop is a
fresh, independent generation session: the loop re-reads the codebase,
organizes context from scratch, and runs the full generation pipeline (change
blocks, go-test, shell, continue, etc.). This is crucial for unattended
operation because each loop starts from the current filesystem state, so the
model's changes from the previous loop are visible in the next one. The goal
command is the "no-human" mode: NoHuman is true, so the loop runs without any
interactive input.

A done block signals goal completion. The model emits a done block when it
believes the goal is achieved, and the goal command stops. Because the done
block is not consumed by any component, it remains in
Result.RemainingBlocks; the goal command checks there.

Malformed blocks that cannot be corrected within the parse-error correction
budget are reported per loop via loops.Result.ParseErrors. Reporting makes
silent change loss — malformed change blocks that are never applied — visible
in unattended operation, where no human is available to notice missing
changes.

The gocodes.CodeProvider is the default for the goal command. The gocodes
pipeline holds no process-level caches: all caches, such as loaded packages
and parsed ASTs, are defined within scope provider functions. Because each
goal loop resolves a fresh GenerateWithResult from a reset scope, dscope.Reset
rebuilds every provider-scoped cache, giving each loop an accurate view of the
current filesystem state.
`

const maxGoalIterations = 20

const GoalSystemPrompt = `
**Goal-Directed Multi-Loop Execution:**

You are working toward a goal that may require multiple independent loops to achieve. Each loop starts fresh: you re-read the codebase from the current filesystem state, analyze the situation, make changes, run tests, and assess progress. Changes from previous loops are visible on disk but not in conversation history.

**Rules:**
- Work toward the goal described in the user input. Make concrete changes (code modifications, tests, documentation) to advance the goal.
- After making changes, assess whether the goal has been fully achieved. Consider: Are all requested changes complete? Do tests pass? Is the code correct and well-structured?
- If the goal is NOT yet achieved, end your turn with a summary block. The system will start another loop with fresh context, allowing you to continue from the current filesystem state.
- If the goal IS achieved, emit a done block, then end with a summary block.

**Goal Completion Signal:**
When you determine the goal is fully achieved, emit a done block:

<<龘靐齉 <done>
goal achieved
龘靐齉

The delimiter 龘靐齉 is illustrative only: choose exactly three uncommon Chinese characters as the delimiter, and use the same delimiter on the closing line.

- Only emit a done block when the goal is genuinely achieved. If unsure, do NOT emit it; continue working in the next loop.
- Each loop is independent: you start fresh with the current filesystem state. Re-read files to verify previous changes before building on them.
- Be thorough: verify your changes with tests (go-test blocks) before declaring the goal achieved.
`

var GoalCommand = Command{
	Defs: []any{
		modes.ForProduction(),
		func() NoHuman { return NoHuman(true) },
		func(
			provider gocodes.CodeProvider,
		) codetypes.CodeProvider {
			return provider
		},
		func(comps codes.CodesComponents) codes.SystemPrompt {
			return codes.SystemPrompt(prompts.Codes + "\n" +
				GoalSystemPrompt + "\n" +
				comps.PromptSections())
		},
	},
	Main: func(
		reset dscope.Reset,
	) {
		ctx := context.Background()

		// achieved records whether a done block was observed. The loop
		// body runs inside a closure passed to scope.Call, so a return
		// there only exits the closure; the flag lets the outer loop
		// stop after the current iteration.
		achieved := false

		for iteration := range maxGoalIterations {
			scope := reset()

			scope.Call(func(
				generateWithResult codes.GenerateWithResult,
			) {

				fmt.Fprintf(os.Stdout, "\n=== Goal Loop %d/%d ===\n\n", iteration+1, maxGoalIterations)

				// Run a full generation cycle. Each call to
				// generateWithResult is independent: it re-reads the
				// codebase, organizes context from scratch, and runs
				// the full generation pipeline (change blocks, go-test,
				// shell, continue, etc.).
				// See TheoryOfGoalCommand.
				result, err := generateWithResult(ctx, os.Stdout)
				if err != nil {
					// Print the error and continue to the next loop.
					// Transient errors (API rate limits) may resolve in
					// the next iteration; persistent errors (missing API
					// key) will repeat and eventually exhaust the limit.
					fmt.Fprintf(os.Stderr, "Goal loop %d failed: %v\n", iteration+1, err)
					return
				}

				// Report malformed blocks that could not be corrected
				// within the parse-error correction budget. In
				// unattended operation, silently dropped change blocks
				// would cause incomplete changes without any signal.
				// See TheoryOfGoalCommand.
				if len(result.ParseErrors) > 0 {
					first := result.ParseErrors[0]
					fmt.Fprintf(os.Stderr,
						"Goal loop %d: %d malformed block(s) could not be corrected (e.g., kind %q boundary %q); some changes may be missing.\n",
						iteration+1, len(result.ParseErrors), first.BlockKind, first.Boundary)
				}

				// Check if the model signaled goal completion by
				// emitting a done block. The done block is not consumed
				// by any component, so it remains in
				// Result.RemainingBlocks. See TheoryOfGoalCommand.
				for _, block := range result.RemainingBlocks {
					if block.Kind == "done" {
						fmt.Fprintf(os.Stdout, "\n=== Goal Achieved after %d loop(s) ===\n", iteration+1)
						achieved = true
						return
					}
				}

			})

			if achieved {
				break
			}
		}

		if !achieved {
			fmt.Fprintf(os.Stdout, "\n=== Goal Not Achieved after %d loops ===\n", maxGoalIterations)
		}
	},
}
