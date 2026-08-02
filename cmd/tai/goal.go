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
The "goal" subcommand implements autonomous goal-directed multi-loop execution.
Unlike "next" which performs a single generation round, or "ai" which is
interactive, "goal" runs multiple independent generation loops until the model
judges the goal as achieved. Each loop is a complete generation cycle with
fresh context organization: the model re-reads the codebase from the current
filesystem state, analyzes the situation, makes changes, runs tests, and
assesses progress toward the goal. This independence ensures each loop starts
from the actual filesystem state (which may have been modified by previous
loops), not from accumulated conversation history. The model is prompted to
evaluate goal completion after each loop; when it determines the goal is
achieved, it emits a done block, and the command exits. A maximum iteration
limit prevents infinite loops when the goal is never achieved.

The design is inspired by autonomous agent goal features where the agent
iteratively works toward an objective, reassessing after each cycle. The key
insight is that each loop is fully independent: no conversation history carries
over between loops, so the model always has an accurate view of the current
codebase state. This prevents context overflow and ensures that changes made by
previous loops are visible to subsequent loops through the filesystem, not
through potentially stale conversation history.

Each loop builds its generation from a fresh reset scope: Main takes
dscope.Reset and invokes it once per iteration, resolving a new
GenerateWithResult for every loop. Reset invalidates per-scope provider caches
(see TheoryOfScopeReset in dscope), so any state captured at
provider-evaluation time is rebuilt per loop instead of being shared across
loops. This makes loop independence a structural guarantee rather than a
convention: even if a provider in the GenerateWithResult chain retains
construction-time state, a failed or interrupted loop cannot leak it into
the next one. The cost is at most one re-evaluation of the provider chain
per loop, bounded by the iteration limit and consistent with the intent
that each loop starts from the current system state. Reset complements the
gocodes.CodeProvider default below: the gocodes pipeline holds no
process-level caches — all caches live inside scope provider functions —
so reset recomputes them on each loop.

The done block is the completion signal, replacing the prior marker-file
approach. The model is instructed to emit a done block (a heredoc-delimited
block with kind "done") when it judges the goal as achieved.
GenerateWithResult returns the loops.Result, which includes RemainingBlocks
containing blocks not consumed by any component. The done block is not matched
by any component, so it remains in RemainingBlocks. Because loops.Run
accumulates unmatched blocks across all rounds within a single
generateWithResult call, the done block is present in the final
Result.RemainingBlocks even if another component triggered additional rounds
in the same loop. After each loop, the goal command scans
Result.RemainingBlocks for a block with Kind "done". If found, the goal is
achieved and the command exits cleanly. This in-band signal eliminates the
need for filesystem side effects and makes the completion signal visible in
the generation output.

The gocodes.CodeProvider is the default for the goal command, matching the go
subcommand's Go-oriented context organization (import distance, AST
transformation). The gocodes pipeline holds no process-level caches: all
caches, such as loaded packages and parsed ASTs, are defined within scope
provider functions. Because each goal loop resolves a fresh
GenerateWithResult from a reset scope, dscope.Reset rebuilds every
provider-scoped cache, giving each loop an accurate view of the current
filesystem state.
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

<<龘靐 <done>
goal achieved
龘靐

The delimiter 龘靐 is illustrative only: choose exactly two uncommon Chinese characters as the delimiter, and use the same delimiter on the closing line.

- Only emit a done block when the goal is genuinely achieved. If unsure, do NOT emit it; continue working in the next loop.
- Each loop is independent: you start fresh with the current filesystem state. Re-read files to verify previous changes before building on them.
- Be thorough: verify your changes with tests (go-test blocks) before declaring the goal achieved.
`

var GoalCommand = Command{
	Defs: []any{
		modes.ForProduction(),
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
