package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/prompts"
	"github.com/reusee/tai/changes"
)

const TheoryOfGoalMode = `
Goal mode autonomously runs the generation pipeline for a set number of
iterations (maxGoalIterations) until a done block is confirmed. Each loop is
a fresh, independent generation session: the loop re-reads the codebase,
organizes context from scratch, and runs the full generation pipeline (change
blocks, go-test, shell, continue, etc.). This is crucial for unattended
operation because each loop starts from the current filesystem state, so the
model's changes from the previous loop are visible in the next one. Goal mode
is a capability of this package, not a separate command: the go subcommand
always enables it (see cmd/tai.TheoryOfGoCommand), so every Go-project
invocation runs the multi-loop mechanism.

A done block is a completion declaration, not a verdict. The model emits a
done block when it believes the goal is achieved; the runner then carries
goalDoneVerificationPrompt into the next loop, which re-reads the current
filesystem state and re-assesses the goal. Only a second consecutive done
block confirms the goal and stops the run. This verification step is required
because the filesystem may change while a loop runs: a loop that loaded
todo.md containing task A cannot see task B added by the user during
execution, so its done declaration may rest on stale context. The
verification loop sees the latest files, so the goal-achieved verdict always
reflects the current state. The verification loop's primary work is
verification and correction: it checks the declaration against the current
state, fixes errors the check uncovers, and starts no unrelated new work.
Because the done block is not consumed by any
component, it remains in Result.RemainingBlocks; the runner checks there.
When the declaration lands on the final budget loop, the runner executes one
extra verification loop beyond the budget. A loop that fails or produces
uncorrected malformed blocks overturns a pending declaration: the goal state
is unknown or changes are missing, so the goal is not achieved.

A successful loop that applied no change blocks ends the run without a
further loop. Within one goal loop the model can chain as many generations
as it needs (continue, shell, go-test blocks), so a loop that ends with an
empty diff set had every opportunity to make changes and concluded without
any; the next loop would read the same filesystem state and repeat the same
analysis. This serves analytical tasks that need no code changes and
complete in one loop. The check runs after done-block handling and only
while no done declaration is pending: a declared goal is still verified even
with no diffs, and a verification loop that corrects nothing falls through
to the clean-loop path so the declaration can still be confirmed. Parse
errors also take precedence: unapplied changes must be re-emitted before the
run may end. The stop prints a dedicated banner and leaves Achieved false —
only a confirmed done block marks achievement; the runner never fabricates
one.

Malformed blocks that cannot be corrected within the parse-error correction
budget are reported per loop via Result.ParseErrors. Reporting makes silent
change loss — malformed change blocks that are never applied — visible in
unattended operation, where no human is available to notice missing changes.

The runner corrects course across loops in unattended operation. When a loop
fails with an error or produces uncorrected malformed blocks, the outcome is
carried into the next loop's system prompt as GoalFeedback, so the model can
correct its approach without a human observer. The feedback is appended at
the end of the system prompt (GoalSystemPromptText), keeping the stable
prefix byte-identical across loops for LLM prefix caching. When the same
error message occurs maxConsecutiveGoalErrors times in a row, the runner
stops early with a diagnostic message instead of burning the remaining
iterations on a persistent failure.

Summaries flow forward alongside feedback: after each loop the runner
appends the loop's non-empty attempt summaries to GoalLoopSummaries and
forks the accumulated list into the next loop's scope, so the model can
reference what earlier loops established without their full transcripts.
GoalSystemPromptText renders the summaries as an append-only section before
the feedback, so the section's growth preserves the byte prefix across
loops for LLM prefix caching.

Each loop opens a fresh dscope scope: GoalLoopGenerator resolves
GenerateWithResultWithStats from a scope rebuilt by dscope.Reset, so every
provider-scoped cache is rebuilt and each loop reads the current filesystem
state. The pipeline holds no process-level caches: all caches, such as loaded
packages and parsed ASTs, live inside scope provider functions.

Each loop prints its attempt statistics at the loop's end (see
TheoryOfAttemptStatistics). The runner accumulates the statistics of every
loop — with AttemptStat.Loop set to the loop number — and prints them once
more, aggregated, after the goal completes. The aggregated report lets the
user review the entire process in a single table: token usage, durations, and
attempt summaries across all loops, with the Loop column identifying which
goal loop produced each attempt.

RunGoal is the plain implementation so tests exercise the loop logic with
fake per-loop generators; Module.GoalRun is the dscope provider that injects
the scope-backed per-loop generator and the review pass. Generation and
review stream to os.Stdout so a TUI's state decorators capture the output
without duplication; banners, verdicts, and the aggregated statistics go to
the runner's output writer.
`

// GoalSystemPrompt teaches the model the goal-directed multi-loop
// protocol: work toward the goal across fresh loops and emit a done
// block only when the goal is genuinely achieved. See TheoryOfGoalMode.
const GoalSystemPrompt = `
**Goal-Directed Multi-Loop Execution:**

You are working toward a goal that may require multiple independent loops to achieve. Each loop starts fresh: you re-read the codebase from the current filesystem state, analyze the situation, make changes, run tests, and assess progress. Changes from previous loops are visible on disk but not in conversation history.

**Rules:**
- Work toward the goal described in the user input. Make concrete changes (code modifications, tests, documentation) to advance the goal.
- After making changes, assess whether the goal has been fully achieved. Consider: Are all requested changes complete? Do tests pass? Is the code correct and well-structured?
- If the goal is NOT yet achieved, end your turn with a summary block. The system will start another loop with fresh context, allowing you to continue from the current filesystem state.
- A loop that ends without applying any change block ends the run: the next loop would see the same filesystem state with nothing new to act on. Complete the goal's changes within the loop — chain generations with continue blocks as needed — before ending the turn.
- If the goal IS achieved, emit a done block, then end with a summary block.

**Goal Completion Signal:**
When you determine the goal is fully achieved, emit a done block (kind "done") whose body states the goal achievement.

- A done block is a completion declaration, not a verdict. The next loop starts fresh, re-reads the current filesystem state — which may have changed while this loop ran (e.g., todo.md may have gained new tasks) — and verifies the declaration. Only when a second consecutive loop also emits a done block is the goal confirmed.
- Only emit a done block when the goal is genuinely achieved. If unsure, do NOT emit it; continue working in the next loop.
- Each loop is independent: you start fresh with the current filesystem state. Re-read files to verify previous changes before building on them.
- Be thorough: verify your changes with tests (go-test blocks) before declaring the goal achieved.
`

// goalDoneVerificationPrompt is the feedback carried into the loop
// immediately after a loop that emitted a done block. A done block is a
// completion declaration, not a verdict: the filesystem may have changed
// since the declaring loop loaded its context (e.g., todo.md may have
// gained new tasks), so the declaration must be verified in a fresh loop
// that re-reads the current filesystem state. Verification is the loop's
// primary work: it re-reads and re-checks, applies corrections only for
// errors the check uncovers, and starts no new work beyond them. Only a
// second consecutive done block confirms the goal. See TheoryOfGoalMode.
const goalDoneVerificationPrompt = `
[System note: The previous goal loop emitted a done block declaring the goal achieved. The filesystem may have changed since that loop loaded its context — for example, todo.md may have been updated with new tasks. Verification is the primary work of this loop: re-read the relevant files (including todo.md) against the CURRENT filesystem state and check whether every task is genuinely complete.

If the check uncovers errors (incorrect or missing changes), fix them; corrections are part of verification. If there is remaining work (e.g., new tasks were added while the previous loop ran), continue working on it in this loop. If the goal is genuinely achieved and nothing needs correction, emit a done block again to confirm. Do not start unrelated new work beyond the check and its corrections.]`

// maxGoalIterations bounds the number of goal loops.
const maxGoalIterations = 20

// maxConsecutiveGoalErrors bounds the number of consecutive goal loops that
// fail with the same error before the run stops early. In unattended
// operation, a persistent failure (e.g., a missing API key or a consistently
// malformed response) would otherwise burn all remaining iterations without
// progress. Stopping early with a diagnostic message surfaces the failure
// while it is fresh. See TheoryOfGoalMode.
const maxConsecutiveGoalErrors = 3

// GoalFeedback carries the outcome of the previous goal loop into the next
// loop's system prompt. In unattended operation, the model cannot ask a
// human what went wrong; the feedback summarizes the previous loop's
// failure (an error or uncorrected malformed blocks) so the next loop can
// correct its approach. The default provider returns empty feedback; the
// goal runner forks the actual value into each loop's scope. See
// TheoryOfGoalMode.
type GoalFeedback string

// GoalLoopSummary is one previous goal loop's attempt summary: the goal
// loop number and the summary text of one attempt within it. See
// TheoryOfGoalMode.
type GoalLoopSummary struct {
	Loop int
	Text string
}

// GoalLoopSummaries carries the attempt summaries of previous goal loops
// into the next loop's system prompt, so the model can reference what
// earlier loops established. The goal runner appends each completed loop's
// non-empty attempt summaries and forks the accumulated list into the next
// loop's scope. See TheoryOfGoalMode.
type GoalLoopSummaries []GoalLoopSummary

// SystemPromptSection renders the summaries as a system prompt section: a
// note stating the section's purpose and one bullet per summary, tagged
// with its goal loop number. An empty list renders the empty string. The
// rendering is append-only across loops: new entries join at the end, so
// an earlier section is a byte prefix of a later one and the LLM prefix
// cache survives loop boundaries. See TheoryOfGoalMode.
func (s GoalLoopSummaries) SystemPromptSection() string {
	if len(s) == 0 {
		return ""
	}
	section := "[System note: The bullets below are the attempt summaries of the previous goal loops, tagged with the loop number. Use them to avoid repeating finished work and to build on established conclusions; the current loop continues from where they left off.]"
	for _, summary := range s {
		section += fmt.Sprintf("\n\n- Loop %d: %s", summary.Loop, summary.Text)
	}
	return section
}

// GoalFeedback provides the default: no feedback. The goal runner forks
// the previous loop's outcome into each loop's scope. See TheoryOfGoalMode.
func (Module) GoalFeedback() GoalFeedback {
	return ""
}

// GoalLoopSummaries provides the default: no previous-loop summaries. The
// goal runner forks the accumulated list into each loop's scope. See
// TheoryOfGoalMode.
func (Module) GoalLoopSummaries() GoalLoopSummaries {
	return nil
}

// GoalOptions carries the runtime configuration of one goal run. Generate
// and Review are injected: Module.GoalRun binds the scope-backed
// implementations; tests inject fakes.
type GoalOptions struct {
	// Output receives loop banners, verdicts, and the aggregated
	// statistics. Generation and review output do not go here: they
	// stream to os.Stdout so a TUI's state decorators capture them
	// without duplication.
	Output io.Writer
	// Generate runs one goal loop: a fresh generation session that
	// re-reads the current filesystem state, with the given feedback and
	// the previous loops' summaries in its system prompt.
	Generate GoalLoopGenerator
	// Review reviews the accumulated session diffs after the run.
	Review RunReview
}

// GoalLoopGenerator runs one goal loop. The feedback and the summaries of
// previous loops reach the loop's system prompt through the goal
// SystemPrompt provider forked by the go command. See TheoryOfGoalMode.
type GoalLoopGenerator func(
	ctx context.Context,
	feedback GoalFeedback,
	summaries GoalLoopSummaries,
) (
	result Result,
	stats []AttemptStat,
	err error,
)

// GoalResult reports the outcome of a goal run. See TheoryOfGoalMode.
type GoalResult struct {
	// Achieved reports whether a done block was confirmed by a
	// verification loop.
	Achieved bool
	// LoopsRun is the number of loops executed, including any extra
	// verification loop beyond the iteration budget.
	LoopsRun int
	// Stats carries the attempt statistics of every loop, with the Loop
	// field identifying the goal loop that produced each attempt.
	Stats []AttemptStat
}

// GoalRun runs the goal loop mechanism: repeated fresh generation loops
// until a done block is confirmed, the iteration budget is exhausted, a
// successful loop applies no change blocks, or the same error repeats,
// followed by a review of the accumulated diffs.
// Banners, verdicts, and aggregated statistics go to output. See
// TheoryOfGoalMode.
type GoalRun func(ctx context.Context, output io.Writer) GoalResult

// GoalSystemPromptText assembles the goal-mode system prompt: the base
// codes prompt, the goal system prompt, and the component sections.
// Previous-loop summaries render as an append-only section, and the
// feedback from the previous loop is appended after them, so the stable
// prefix remains byte-identical across loops for LLM prefix caching. The
// go command forks this as its SystemPrompt provider; see
// cmd/tai.TheoryOfGoCommand.
func GoalSystemPromptText(comps CodesComponents, feedback GoalFeedback, summaries GoalLoopSummaries) SystemPrompt {
	prompt := prompts.Codes + "\n" +
		GoalSystemPrompt + "\n" +
		comps.PromptSections()
	if section := summaries.SystemPromptSection(); section != "" {
		prompt += "\n\n" + section
	}
	if feedback != "" {
		prompt += "\n\n" + string(feedback)
	}
	return SystemPrompt(prompt)
}

// goalLoopState carries the mutable state of a goal run across loops: the
// feedback for the next loop's system prompt, the attempt summaries of
// previous loops, the pending done-block declaration, repeated-error
// tracking, and the terminal flags. See TheoryOfGoalMode.
type goalLoopState struct {
	feedback                GoalFeedback
	summaries               GoalLoopSummaries
	pendingDoneVerification bool
	lastErrMsg              string
	consecutiveErrors       int
	achieved                bool
	stopRequested           bool
}

// applyLoopResult folds one loop's outcome into the runner state and
// reports whether the run should stop after this loop. See
// TheoryOfGoalMode.
func (s *goalLoopState) applyLoopResult(
	loopsRun int,
	result Result,
	err error,
	output io.Writer,
) bool {
	if err != nil {
		return s.applyLoopError(loopsRun, err)
	}
	return s.applyLoopSuccess(loopsRun, result, output)
}

// applyLoopError folds a failed loop into the runner state: a failure
// overturns a pending done declaration and carries corrective feedback
// into the next loop; the same error repeated maxConsecutiveGoalErrors
// times in a row stops the run. See TheoryOfGoalMode.
func (s *goalLoopState) applyLoopError(loopsRun int, err error) bool {
	fmt.Fprintf(os.Stderr, "Goal loop %d failed: %v\n", loopsRun, err)

	errMsg := err.Error()
	if errMsg == s.lastErrMsg {
		s.consecutiveErrors++
	} else {
		s.consecutiveErrors = 1
		s.lastErrMsg = errMsg
	}
	if s.consecutiveErrors >= maxConsecutiveGoalErrors {
		fmt.Fprintf(os.Stderr,
			"\n=== Goal Stopped: the same error occurred %d consecutive times ===\n%s\n",
			maxConsecutiveGoalErrors, errMsg)
		s.stopRequested = true
		return true
	}

	s.pendingDoneVerification = false

	s.feedback = GoalFeedback(fmt.Sprintf(
		"[System note: The previous goal loop failed: %v\nCorrect the cause of this error in this loop and continue from the current filesystem state.]",
		err))
	return false
}

// applyLoopSuccess folds a successful loop into the runner state:
// uncorrected malformed blocks carry re-emit feedback and overturn a
// pending done declaration; a done block is a declaration that the next
// loop verifies; a loop that applied no change blocks ends the run; a
// clean loop with changes clears the feedback. See TheoryOfGoalMode.
func (s *goalLoopState) applyLoopSuccess(loopsRun int, result Result, output io.Writer) bool {
	s.consecutiveErrors = 0
	s.lastErrMsg = ""

	if len(result.ParseErrors) > 0 {
		first := result.ParseErrors[0]
		fmt.Fprintf(os.Stderr,
			"Goal loop %d: %d malformed block(s) could not be corrected (e.g., kind %q boundary %q); some changes may be missing.\n",
			loopsRun, len(result.ParseErrors), first.BlockKind, first.Boundary)
		s.pendingDoneVerification = false

		s.feedback = GoalFeedback(fmt.Sprintf(
			"[System note: The previous goal loop produced %d malformed block(s) that could not be corrected, e.g. kind %q with boundary %q. These blocks were NOT applied. In this loop, re-emit ONLY the corrected versions of these blocks; do not re-emit blocks that were applied successfully.]",
			len(result.ParseErrors), first.BlockKind, first.Boundary))
		return false
	}

	foundDone := false
	for _, block := range result.RemainingBlocks {
		if block.Kind == "done" {
			foundDone = true
			break
		}
	}

	if foundDone {
		if s.pendingDoneVerification {
			fmt.Fprintf(output, "\n=== Goal Achieved after %d loop(s) ===\n", loopsRun)
			s.achieved = true
			return true
		}
		s.pendingDoneVerification = true
		s.feedback = GoalFeedback(goalDoneVerificationPrompt)
		return false
	}

	// A loop that applied no change blocks ends the run: within one loop
	// the model can chain as many generations as it needs, so an empty
	// diff set means it concluded without changes, and the next loop
	// would read the same filesystem state and repeat the same analysis.
	// The check is skipped while a done declaration is pending: that
	// loop verifies the declaration, and a verification without
	// corrections falls through to the clean-loop path below so the
	// declaration can still be confirmed. See TheoryOfGoalMode.
	if !s.pendingDoneVerification && len(result.Diffs) == 0 {
		fmt.Fprintf(output,
			"\n=== Goal Run Complete: loop %d applied no change blocks ===\n", loopsRun)
		s.stopRequested = true
		return true
	}

	s.pendingDoneVerification = false
	s.feedback = ""
	return false
}

// RunGoal runs the goal loop mechanism: repeated fresh generation loops
// until a done block is confirmed, the iteration budget is exhausted, a
// successful loop applies no change blocks, or the same error repeats,
// followed by a review of the accumulated diffs.
// See TheoryOfGoalMode.
func RunGoal(ctx context.Context, opts GoalOptions) GoalResult {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

	state := &goalLoopState{}
	var allStats []AttemptStat
	var allDiffs []changes.FileDiff
	loopsRun := 0

	// runOneLoop executes one generation loop and folds its outcome into
	// the runner state. It reports whether the run should stop after the
	// loop.
	runOneLoop := func() bool {
		loopStart := len(allStats)
		result, stats, err := opts.Generate(ctx, state.feedback, state.summaries)
		allStats = append(allStats, stats...)
		for i := loopStart; i < len(allStats); i++ {
			allStats[i].Loop = loopsRun
		}
		state.summaries = appendLoopSummaries(state.summaries, loopsRun, allStats[loopStart:])
		allDiffs = append(allDiffs, result.Diffs...)
		return state.applyLoopResult(loopsRun, result, err, opts.Output)
	}

	for loopsRun < maxGoalIterations {
		loopsRun++
		fmt.Fprintf(opts.Output, "\n=== Goal Loop %d/%d ===\n\n", loopsRun, maxGoalIterations)
		if runOneLoop() {
			break
		}
	}

	// A done block declaration on the final budget loop is still just a
	// declaration: it must be verified in a fresh loop that sees the
	// latest filesystem state. Run one extra verification loop beyond the
	// budget.
	if state.pendingDoneVerification && !state.achieved && !state.stopRequested {
		loopsRun++
		fmt.Fprintf(opts.Output, "\n=== Goal Verification Loop %d (beyond budget) ===\n\n", loopsRun)
		runOneLoop()
	}

	if !state.achieved && !state.stopRequested {
		fmt.Fprintf(opts.Output, "\n=== Goal Not Achieved after %d loops ===\n", loopsRun)
	}

	if err := opts.Review(ctx, os.Stdout, allDiffs); err != nil {
		fmt.Fprintf(os.Stderr, "Review failed: %v\n", err)
	}

	if len(allStats) > 0 {
		PrintAttemptStats(opts.Output, allStats, "Goal Loop Statistics")
	}

	return GoalResult{
		Achieved: state.achieved,
		LoopsRun: loopsRun,
		Stats:    allStats,
	}
}

// appendLoopSummaries appends the non-empty attempt summaries of one goal
// loop to the accumulated list, tagging each entry with the goal loop
// number. Attempts without a summary (e.g., failed or truncated attempts)
// contribute nothing. See TheoryOfGoalMode.
func appendLoopSummaries(summaries GoalLoopSummaries, loop int, stats []AttemptStat) GoalLoopSummaries {
	for _, stat := range stats {
		if stat.Summary == "" {
			continue
		}
		summaries = append(summaries, GoalLoopSummary{Loop: loop, Text: stat.Summary})
	}
	return summaries
}

// makeGoalLoopGenerator builds the per-loop generation function: each call
// opens a fresh scope via reset, forks the loop's feedback and the
// previous loops' summaries into it, and resolves
// GenerateWithResultWithStats from the forked scope, so the loop reads the
// latest filesystem state and sees the previous loops' feedback and
// summaries in its system prompt. Generation streams to os.Stdout: in TUI
// mode the terminal's stdout is the null device and the display captures
// output through state decorators, so a per-loop writer would duplicate
// output. See TheoryOfGoalMode.
func makeGoalLoopGenerator(reset dscope.Reset) GoalLoopGenerator {
	return func(ctx context.Context, feedback GoalFeedback, summaries GoalLoopSummaries) (Result, []AttemptStat, error) {
		scope := reset()
		if feedback != "" {
			scope = scope.Fork(func() GoalFeedback { return feedback })
		}
		if len(summaries) > 0 {
			scope = scope.Fork(func() GoalLoopSummaries { return summaries })
		}
		var result Result
		var stats []AttemptStat
		var err error
		scope.Call(func(generate GenerateWithResultWithStats) {
			result, stats, err = generate(ctx, os.Stdout)
		})
		return result, stats, err
	}
}

// GoalRun provider: runs the goal loop mechanism with the scope-backed
// per-loop generator and the review pass. Each GoalRun resolution binds
// reset to the resolving scope, so the command's forks (parts provider,
// goal system prompt) apply to every loop. See TheoryOfGoalMode.
func (Module) GoalRun(
	reset dscope.Reset,
	runReview RunReview,
) GoalRun {
	return func(ctx context.Context, output io.Writer) GoalResult {
		return RunGoal(ctx, GoalOptions{
			Output:   output,
			Generate: makeGoalLoopGenerator(reset),
			Review:   runReview,
		})
	}
}
