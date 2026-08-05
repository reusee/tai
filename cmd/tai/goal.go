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

The goal command also corrects course across loops in unattended operation.
When a loop fails with an error or produces uncorrected malformed blocks, the
outcome is carried into the next loop's system prompt as GoalFeedback, so the
model can correct its approach without a human observer. The feedback is
appended at the end of the system prompt, keeping the stable prefix
byte-identical across loops for LLM prefix caching. When the same error
message occurs maxConsecutiveGoalErrors times in a row, the goal command stops
early with a diagnostic message instead of burning the remaining iterations on
a persistent failure.

The gocodes.CodeProvider is the default for the goal command. The gocodes
pipeline holds no process-level caches: all caches, such as loaded packages
and parsed ASTs, are defined within scope provider functions. Because each
goal loop resolves a fresh GenerateWithResultWithStats from a reset scope,
dscope.Reset rebuilds every provider-scoped cache, giving each loop an
accurate view of the current filesystem state.

Each goal loop prints its round statistics at the loop's end (see
codes.TheoryOfRoundStatistics). In addition, the goal command accumulates the
statistics of every loop — via the statistics-returning
codes.GenerateWithResultWithStats, with codes.RoundStat.Loop set to the loop
number — and prints them once more, aggregated, after the goal completes. The
aggregated report lets the user review the entire process in a single table:
token usage, durations, and round summaries across all loops, with the Loop
column identifying which goal loop produced each round.
`

const maxGoalIterations = 20

// maxConsecutiveGoalErrors bounds the number of consecutive goal loops that
// fail with the same error before the goal command stops early. In unattended
// operation, a persistent failure (e.g., a missing API key or a consistently
// malformed response) would otherwise burn all remaining iterations without
// progress. Stopping early with a diagnostic message surfaces the failure
// while it is fresh. See TheoryOfGoalCommand.
const maxConsecutiveGoalErrors = 3

// GoalFeedback carries the outcome of the previous goal loop into the next
// loop's system prompt. In unattended operation, the model cannot ask a human
// what went wrong; the feedback summarizes the previous loop's failure (an
// error or uncorrected malformed blocks) so the next loop can correct its
// approach. The default provider returns empty feedback; GoalCommand.Main
// forks the loop scope with the actual feedback value before each iteration.
// See TheoryOfGoalCommand.
type GoalFeedback string

func (Module) GoalFeedback() GoalFeedback {
	return ""
}

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
		func(
			comps codes.CodesComponents,
			feedback GoalFeedback,
		) codes.SystemPrompt {
			prompt := prompts.Codes + "\n" +
				GoalSystemPrompt + "\n" +
				comps.PromptSections()
			// Feedback from the previous loop is appended at the end of
			// the system prompt so the stable prefix (base prompt, goal
			// prompt, component sections) remains byte-identical across
			// loops for LLM prefix caching; only the feedback suffix
			// changes. See TheoryOfGoalCommand.
			if feedback != "" {
				prompt += "\n\n" + string(feedback)
			}
			return codes.SystemPrompt(prompt)
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

		// stopRequested records whether the goal loop should stop after
		// the current iteration, e.g., when the same error has occurred
		// maxConsecutiveGoalErrors times in a row. The loop body runs
		// inside a closure passed to scope.Call, so a return there only
		// exits the closure; the flag lets the outer loop stop after the
		// current iteration. See TheoryOfGoalCommand.
		stopRequested := false

		// feedback carries the previous loop's outcome into the next
		// loop's system prompt via GoalFeedback, so the model can correct
		// its approach in unattended operation without a human observer.
		// See TheoryOfGoalCommand.
		var feedback GoalFeedback

		// Repeated-error detection: the same error message across
		// consecutive loops indicates a persistent failure that further
		// loops are unlikely to resolve. See TheoryOfGoalCommand.
		var lastErrMsg string
		consecutiveErrors := 0

		// allStats accumulates the round statistics of every goal loop so
		// they can be printed once more, aggregated, after the goal
		// completes — in addition to the per-loop print at each loop's end.
		// The Loop field is set to the loop number when each loop's stats
		// are appended, so the aggregated table shows the entire process at
		// a glance. See codes.TheoryOfRoundStatistics.
		var allStats []codes.RoundStat

		for iteration := range maxGoalIterations {
			scope := reset()
			if feedback != "" {
				scope = scope.Fork(func() GoalFeedback { return feedback })
			}

			scope.Call(func(
				generateWithResultWithStats codes.GenerateWithResultWithStats,
			) {

				fmt.Fprintf(os.Stdout, "\n=== Goal Loop %d/%d ===\n\n", iteration+1, maxGoalIterations)

				// Run a full generation cycle. Each call to
				// generateWithResultWithStats is independent: it re-reads the
				// codebase, organizes context from scratch, and runs
				// the full generation pipeline (change blocks, go-test,
				// shell, continue, etc.). It returns the loop result
				// together with the round statistics collected during the
				// loop, which are retained for the aggregated final report.
				// See TheoryOfGoalCommand and codes.TheoryOfRoundStatistics.
				loopStart := len(allStats)
				result, stats, err := generateWithResultWithStats(ctx, os.Stdout)
				// Retain this loop's statistics for the aggregated final
				// report. The Loop field identifies the goal loop that
				// produced each round, so the final table shows the entire
				// process at a glance. See codes.TheoryOfRoundStatistics.
				allStats = append(allStats, stats...)
				for i := loopStart; i < len(allStats); i++ {
					allStats[i].Loop = iteration + 1
				}
				if err != nil {
					// Print the error and continue to the next loop.
					// Transient errors (API rate limits) may resolve in
					// the next iteration; persistent errors (missing API
					// key) will repeat and eventually exhaust the limit.
					fmt.Fprintf(os.Stderr, "Goal loop %d failed: %v\n", iteration+1, err)

					// Detect repeated identical errors and stop early. In
					// unattended operation, a persistent failure burns
					// iterations without progress; stopping with a
					// diagnostic lets the operator investigate.
					// See TheoryOfGoalCommand.
					errMsg := err.Error()
					if errMsg == lastErrMsg {
						consecutiveErrors++
					} else {
						consecutiveErrors = 1
						lastErrMsg = errMsg
					}
					if consecutiveErrors >= maxConsecutiveGoalErrors {
						fmt.Fprintf(os.Stderr,
							"\n=== Goal Stopped: the same error occurred %d consecutive times ===\n%s\n",
							maxConsecutiveGoalErrors, errMsg)
						stopRequested = true
						return
					}

					// Carry the failure into the next loop's system prompt
					// so the model can correct the cause and continue from
					// the current filesystem state. Changes from successful
					// rounds of the failed loop are already applied; only
					// the failed round's changes were discarded.
					// See TheoryOfGoalCommand.
					feedback = GoalFeedback(fmt.Sprintf(
						"[System note: The previous goal loop failed: %v\nCorrect the cause of this error in this loop and continue from the current filesystem state.]",
						err))
					return
				}

				// The loop succeeded; reset repeated-error tracking so that
				// only consecutive failures count toward
				// maxConsecutiveGoalErrors. See TheoryOfGoalCommand.
				consecutiveErrors = 0
				lastErrMsg = ""

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

					// Carry the uncorrected malformed blocks into the next
					// loop's system prompt so the model can re-emit them in
					// corrected form. In unattended operation, the next loop
					// is the model's only chance to fix them.
					// See TheoryOfGoalCommand.
					feedback = GoalFeedback(fmt.Sprintf(
						"[System note: The previous goal loop produced %d malformed block(s) that could not be corrected, e.g., kind %q with boundary %q. These blocks were NOT applied. In this loop, re-emit ONLY the corrected versions of these blocks; do not re-emit blocks that were applied successfully.]",
						len(result.ParseErrors), first.BlockKind, first.Boundary))
				} else {
					feedback = ""
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

			if achieved || stopRequested {
				break
			}
		}

		if !achieved && !stopRequested {
			fmt.Fprintf(os.Stdout, "\n=== Goal Not Achieved after %d loops ===\n", maxGoalIterations)
		}

		// Print all loop statistics once more, aggregated, after the goal
		// completes so the user can review the entire process in a single
		// table. The per-loop print at each loop's end remains. The Loop
		// column identifies which goal loop produced each round.
		// See codes.TheoryOfRoundStatistics.
		if len(allStats) > 0 {
			codes.PrintRoundStats(os.Stdout, allStats, "Goal Loop Statistics")
		}
	},
}
