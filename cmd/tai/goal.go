package main

import (
	"context"
	"fmt"
	"os"

	"github.com/reusee/prompts"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/modes"
)

const TheoryOfGoalCommand = `
The "goal" subcommand implements autonomous goal-directed multi-loop execution.
Unlike "next" which performs a single generation round, or "ai" which is
interactive, "goal" runs multiple independent generation loops until the model
judges the goal as achieved. Each loop is a complete codes.Generate cycle with
fresh context organization: the model re-reads the codebase from the current
filesystem state, analyzes the situation, makes changes, runs tests, and
assesses progress toward the goal. This independence ensures each loop starts
from the actual filesystem state (which may have been modified by previous
loops), not from accumulated conversation history. The model is prompted to
evaluate goal completion after each loop; when it determines the goal is
achieved, it emits a WRITE change block creating a .GOAL_COMPLETE marker file,
and the command exits. A maximum iteration limit prevents infinite loops when
the goal is never achieved.

The design is inspired by autonomous agent goal features where the agent
iteratively works toward an objective, reassessing after each cycle. The key
insight is that each loop is fully independent: no conversation history carries
over between loops, so the model always has an accurate view of the current
codebase state. This prevents context overflow and ensures that changes made by
previous loops are visible to subsequent loops through the filesystem, not
through potentially stale conversation history.

The .GOAL_COMPLETE marker file is the completion signal. The model is
instructed to create it via a WRITE change block when it judges the goal as
achieved. The goal command checks for this file after each codes.Generate call.
If found, the goal is achieved and the command exits cleanly. If not found,
another loop is started. The marker file is removed before each loop to prevent
false positives from previous iterations.

The anytexts.CodeProvider is used instead of gocodes.CodeProvider to avoid the
sync.OnceValues cache in GetFiles. The gocodes pipeline caches loaded packages
and parsed ASTs for the lifetime of the process, which is correct for
single-shot commands (go, any) but incorrect for the goal command where each
loop must see the current filesystem state. The anytexts.CodeProvider reads
from disk on every call, ensuring each loop sees changes made by previous
loops. This trades Go-specific context simplification (import distance, AST
transformation) for filesystem freshness, which is the correct trade-off for
iterative goal-directed execution.
`

const maxGoalIterations = 20

const goalCompleteMarker = ".GOAL_COMPLETE"

const GoalSystemPrompt = `
**Goal-Directed Multi-Loop Execution:**

You are working toward a goal that may require multiple independent loops to achieve. Each loop starts fresh: you re-read the codebase from the current filesystem state, analyze the situation, make changes, run tests, and assess progress. Changes from previous loops are visible on disk but not in conversation history.

**Rules:**
- Work toward the goal described in the user input. Make concrete changes (code modifications, tests, documentation) to advance the goal.
- After making changes, assess whether the goal has been fully achieved. Consider: Are all requested changes complete? Do tests pass? Is the code correct and well-structured?
- If the goal is NOT yet achieved, end your turn with a summary block. The system will start another loop with fresh context, allowing you to continue from the current filesystem state.
- If the goal IS achieved, emit a WRITE change block to create a .GOAL_COMPLETE marker file, then end with a summary block.

**Goal Completion Signal:**
When you determine the goal is fully achieved, emit this change block:

<<龘靐 <change op="WRITE" file-path=".GOAL_COMPLETE">
goal achieved
龘靐

The delimiter 龘靐 is illustrative only: choose exactly two uncommon Chinese characters as the delimiter, and use the same delimiter on the closing line.

- Only emit .GOAL_COMPLETE when the goal is genuinely achieved. If unsure, do NOT emit it; continue working in the next loop.
- Each loop is independent: you start fresh with the current filesystem state. Re-read files to verify previous changes before building on them.
- Be thorough: verify your changes with tests (go-test blocks) before declaring the goal achieved.
`

var GoalCommand = Command{
	Defs: []any{
		modes.ForProduction(),
		func(
			provider anytexts.CodeProvider,
		) codetypes.CodeProvider {
			return provider
		},
		func(comps codes.CodesComponents) codes.SystemPrompt {
			return codes.SystemPrompt(prompts.Codes + "\n" +
				GoalSystemPrompt + "\n" +
				comps.PromptSections() +
				comps.RestatePrompts())
		},
	},
	Main: func(
		generate codes.Generate,
	) {
		ctx := context.Background()

		for iteration := range maxGoalIterations {
			// Remove stale completion marker from previous iterations.
			os.Remove(goalCompleteMarker)

			fmt.Fprintf(os.Stdout, "\n=== Goal Loop %d/%d ===\n\n", iteration+1, maxGoalIterations)

			// Run a full generation cycle. Each call to generate is
			// independent: it re-reads the codebase, organizes context
			// from scratch, and runs the full generation pipeline
			// (change blocks, go-test, shell, continue, etc.).
			// See TheoryOfGoalCommand.
			if err := generate(ctx, os.Stdout); err != nil {
				// Print the error and continue to the next loop.
				// Transient errors (API rate limits) may resolve in
				// the next iteration; persistent errors (missing API
				// key) will repeat and eventually exhaust the limit.
				fmt.Fprintf(os.Stderr, "Goal loop %d failed: %v\n", iteration+1, err)
				continue
			}

			// Check if the model signaled goal completion by creating
			// the .GOAL_COMPLETE marker file via a WRITE change block.
			// The MemoryStore in codes.Generate flushes change blocks
			// to disk on round success, so the marker file is present
			// on disk after a successful round that included the
			// WRITE block. See TheoryOfGoalCommand.
			if _, err := os.Stat(goalCompleteMarker); err == nil {
				os.Remove(goalCompleteMarker)
				fmt.Fprintf(os.Stdout, "\n=== Goal Achieved after %d loop(s) ===\n", iteration+1)
				return
			}
		}

		fmt.Fprintf(os.Stdout, "\n=== Goal Not Achieved after %d loops ===\n", maxGoalIterations)
	},
}
