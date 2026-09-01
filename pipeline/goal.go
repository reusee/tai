package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/reusee/dscope"
	"github.com/reusee/prompts"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/flags"
)

const TheoryOfGoalMode = `
Goal mode autonomously runs the generation pipeline for a set number of
iterations (maxGoalIterations) until a loop emits a done block while
applying no change blocks. Each loop is a fresh, independent generation
session: the loop re-reads the codebase, organizes context from scratch,
and runs the full generation pipeline (change blocks, go-test, shell,
continue, etc.). This is crucial for unattended operation because each
loop starts from the current filesystem state, so the model's changes
from the previous loop are visible in the next one. Goal mode is a
capability of this package, not a separate command: the auto-detected
default command inside a Go module always enables it (see
cmd/tai.TheoryOfGoModuleDefault), so every Go-project invocation runs the
multi-loop mechanism.

The run ends only on a loop that applied no change blocks and emitted a
done block; that block is the run's only exit. A loop that applied
change blocks never ends the run — even when it also emits a done
block — because its changes must be checked by a loop that reads the
resulting filesystem state: a done block emitted together with change
blocks is not a completion signal but a declaration the next loop must
verify. The runner carries goalDoneVerificationPrompt into that next
loop, whose primary work is verification and correction: it re-reads
the current filesystem state, checks the declaration and the changes,
fixes the errors the check uncovers, and starts no unrelated new work.
Verification is a gap analysis, not only a correctness check: the loop
compares the original goal from the user input against the current
state, requirement by requirement, checking what was NOT done as well
as what was done — a done block is warranted only when the analysis
finds no gap, no incorrect change and no missing requirement. The
verification is required because the filesystem may change while a loop
runs: a loop cannot see requirements the user added while it ran, so
its done declaration may rest on stale context. A verification loop's
own corrections are new changes
that the following loop verifies in turn, so the cycle repeats until a
loop examines the current state and finds nothing to correct: that loop
emits a done block and applies no change blocks, and the run ends
achieved. Because the done block is not consumed by any component, it
remains in Result.RemainingBlocks; the runner checks there. When a
declaration with changes lands on the final budget loop, the runner
executes one extra verification loop beyond the budget. A loop that
fails or produces uncorrected malformed blocks carries corrective
feedback into the next loop: the goal state is unknown or changes are
missing, so the goal is not achieved.

A change-free loop that emitted a done block achieves the goal and ends
the run; analytical tasks that need no code changes end the same way,
with an explicit done block. A change-free loop without a done block
does not end the run: the combination is a model output failure — the
loop produced neither progress nor the completion signal — and its
plausible cause is a severe defect in that loop's own conversation
history, which cannot be repaired from within the loop. Because every
loop is independent (fresh scope, fresh context), the runner reports
the failure, carries goalNoDoneFeedbackPrompt into the next loop, and
continues: the fresh loop re-reads the filesystem and can both recover
the assessment and emit the done block the silent loop failed to emit.
A pending done declaration survives a silent loop — the silent loop
verified nothing — so the beyond-budget verification loop still runs
when the budget ends with an unverified declaration. Parse errors take
precedence: unapplied changes must be re-emitted before the run may
end. The runner never fabricates a done block; when the iteration
budget is exhausted without one, the run ends unachieved.

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

Each loop's number is forked into the loop's scope as GoalLoop and passed
to RunOptions.Loop, so the generation loop stamps every event it emits
with the loop number (see TheoryOfLoopEvents). The runner accumulates the
attempt statistics of every loop — with AttemptStat.Loop set to the loop
number — into GoalResult.Stats, so a caller can review the entire
process in a single view: token usage, durations, and attempt summaries
across all loops, with the Loop field identifying which goal loop
produced each attempt.

RunGoal is the plain implementation so tests exercise the loop logic with
fake per-loop generators; Module.GoalRun is the dscope provider that injects
the scope-backed per-loop generator and the review pass. Generation and
review stream to os.Stdout so a TUI's state decorators capture the output
without duplication. Verdicts and failure notes are routed through
goalReporter: as EventGoal events through GoalEventObserver when one is
set (a display front-end's Events tab), or written to the output writer
(failure notes to stderr) otherwise.
`

// TheoryOfGoalReviewModel documents the model used by the goal loops after a
// done block has been emitted. See TheoryOfGoalMode.
const TheoryOfGoalReviewModel = `
The loops after the first done block emitted together with change
blocks run on the review models: the goal runner forks one configured
review model (ReviewModels) into each post-done loop's scope as
flags.ModelName — the same fork Module.RunReview uses for its review
sessions — so the model that verifies and corrects the declared goal is
independent of the model that made the changes. The first done block
itself is emitted by the default model. The selection advances one
model per done block: the first done block selects the first review
model for the loop that verifies it, and each later done block selects
the next model; reaching the last model fixes the selection, so later
done blocks keep it. A loop without a done block — corrections, errors,
malformed blocks — does not advance the selection, so the phase is
sticky however the corrections unfold. When no review model is
configured, post-done loops keep the default model.
`

// GoalSystemPrompt teaches the model the goal-directed multi-loop protocol:
// work toward the goal across fresh loops and end the run by emitting a
// done block from a loop that applies no change blocks — the done block
// is the run's only exit. Inquiry tasks end the same way: the loop that
// delivers the answer emits a done block with no change blocks. A loop
// that ends with neither change blocks nor a done block is a model output
// failure and never ends the run. The completion assessment is a gap
// analysis: what was NOT done is checked against the original goal as
// well as the correctness of what was done.
// See TheoryOfGoalMode.
const GoalSystemPrompt = `
**Goal-Directed Multi-Loop Execution:**

You are working toward a goal that may require multiple independent loops to achieve. Each loop starts fresh: you re-read the codebase from the current filesystem state, analyze the situation, make changes, run tests, and assess progress. Changes from previous loops are visible on disk but not in conversation history.

**Rules:**
- Work toward the goal described in the user input. Make concrete changes (code modifications, tests, documentation) to advance the goal.
- After making changes, assess whether the goal has been fully achieved. The assessment is a gap analysis, not just a correctness check: verify what was NOT done as well as what was done — compare the original goal in the user input against the current state, requirement by requirement, and identify anything still missing. Consider: Are all requested changes complete? Do tests pass? Is the code correct and well-structured?
- Economize rounds without sacrificing correctness: each response is one model round. Batch context fetches — every symbol you need in one go-src block, every file in one ingest block — and emit change blocks together with the go-test blocks that verify them in the same response, so each round completes as much work as possible.
- If the goal is NOT yet achieved, end your turn with a summary block. The system will start another loop with fresh context, allowing you to continue from the current filesystem state.
- A loop that ends without applying any change block and without emitting a done block does NOT end the run: that combination is a model output failure — the loop produced neither progress nor the completion signal, typically because something went severely wrong inside that loop's own conversation history. The system starts another loop with corrective feedback and a fresh context, so every loop must end one of two ways: change blocks applied, or a done block when the gap analysis finds no gap. Complete the goal's changes within the loop — chain generations with continue blocks as needed — before ending the turn.
- If the goal IS achieved and this loop found nothing to correct, emit a done block, then end with a summary block.
- Inquiry tasks are goals too: when the work is analytical or informational — answering a question, reviewing or explaining code, writing a report — and needs no change blocks, the loop that delivers the answer is achieved. End it with a done block whose body states the conclusion, applying no change blocks; a summary block alone never ends the run.

**Goal Completion Signal:**
When you determine the goal is fully achieved, emit a done block (kind "done") whose body states the goal achievement.

- Before emitting a done block, perform a gap analysis: enumerate the original goal's requirements and check each against the current filesystem state. The done block is warranted only when the analysis finds no gap — every requirement satisfied, nothing incorrect, nothing missing. Verifying the correctness of what was done is not sufficient; the completeness of what was not done must be verified too.
- Emitting the done block is the only way the goal run ends. The run ends only when a loop emits a done block AND applies no change blocks in that same loop. A loop that applies change blocks never ends the run — even when it also emits a done block: its changes must be verified by the next loop, which re-reads the current filesystem state, checks the changes, and corrects any errors it finds.
- A done block emitted together with change blocks is therefore not a completion signal: it asks the next loop to verify those changes, and the corrections made by a verification loop are verified in turn by the following loop. The cycle repeats until a loop examines the current state and finds nothing to correct — that loop emits a done block and applies no change blocks, ending the run.
- Only emit a done block when the goal is genuinely achieved and this loop corrected nothing. If unsure, do NOT emit it; continue working in the next loop.
- Each loop is independent: you start fresh with the current filesystem state. Re-read files to verify previous changes before building on them.
- Be thorough: verify your changes with tests (go-test blocks) before declaring the goal achieved.
`

// goalDoneVerificationPrompt is the feedback carried into the loop
// immediately after a loop that emitted a done block while applying
// change blocks. The declaration is not final: a loop that applied
// change blocks never ends the run, so the next loop verifies the
// declaration and the changes against the current filesystem state.
// Verification is a gap analysis: the loop checks what was NOT done
// against the original goal as well as the correctness of what was
// done, corrects what the check uncovers, and starts no unrelated new
// work. Only a loop that emits a done block and applies no change
// blocks ends the run. See TheoryOfGoalMode.
const goalDoneVerificationPrompt = `
[System note: The previous goal loop applied change blocks and declared the goal achieved with a done block. The declaration is not final: a loop that applies change blocks never ends the run. Verification is the primary work of this loop: re-read the relevant files against the CURRENT filesystem state and check whether every task is genuinely complete and every applied change is correct.

Verification must cover both what was done and what was NOT done: compare the original goal in the user input against the current state, requirement by requirement. Checking the correctness of the applied changes is not sufficient — enumerate the goal's requirements and confirm the current filesystem state satisfies each one, so no part of the goal is left unimplemented.

If the check uncovers errors (incorrect or missing changes) or any gap — a requirement of the original goal that the current state does not satisfy — fix or complete it; corrections are part of verification, and the next loop will verify them in turn. If there is remaining work (e.g., new tasks were added while the previous loop ran), continue working on it in this loop. If the goal is genuinely achieved and the gap analysis finds no gap — every requirement satisfied, nothing incorrect, nothing missing — emit a done block and apply no change blocks — only such a loop ends the run. Do not start unrelated new work beyond the check and its corrections.]`

// goalNoDoneFeedbackPrompt is the feedback carried into the loop after
// a loop that applied no change blocks and emitted no done block. The
// combination is a model output failure: the loop produced neither
// progress nor the completion signal, and its plausible cause is a
// severe defect in that loop's own conversation history, which cannot
// be repaired from within the loop. The next loop starts fresh, so the
// runner carries this corrective feedback and continues: the loop
// redoes the assessment from the current filesystem state, and the done
// block remains the only run exit. See TheoryOfGoalMode.
const goalNoDoneFeedbackPrompt = `
[System note: The previous goal loop ended with NO change blocks and NO done block. That combination is a model output failure: the loop produced neither progress nor the completion signal the protocol requires, which usually means something went severely wrong inside that loop's own conversation history — a defect that cannot be repaired from within it. This loop starts fresh: the corrupted history is gone, and the current filesystem state is the only ground truth.

Redo the assessment from scratch: re-read the relevant files against the original goal in the user input, requirement by requirement — checking what was NOT done as well as the correctness of what was done. If any requirement is unmet, or any applied change is incorrect or incomplete, fix or complete it with change blocks. If — and only if — the gap analysis finds no gap (every requirement satisfied, nothing incorrect, nothing missing), emit a done block: emitting the done block is the only way the goal run ends. Never end a loop with neither change blocks nor a done block.]`

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

// GoalEventObserver provides the default: no observer. The goal runner
// then writes verdicts to the output writer. A display front-end (e.g.,
// tai's TUI) forks this provider with its event handler, so the goal
// verdicts render in its Events tab. See TheoryOfGoalMode.
func (Module) GoalEventObserver() GoalEventObserver {
	return nil
}

// GoalLoop provides the default: zero, a non-goal run. The goal runner
// forks each loop's number into the loop's scope. See TheoryOfGoalMode.
func (Module) GoalLoop() GoalLoop { return 0 }

// GoalOptions carries the runtime configuration of one goal run. Generate
// and Review are injected: Module.GoalRun binds the scope-backed
// implementations; tests inject fakes.
type GoalOptions struct {
	// Output receives verdicts and failure notes when no GoalEvents
	// observer is set. Generation and review output do not go here:
	// they stream to os.Stdout so a TUI's state decorators capture
	// them without duplication.
	Output io.Writer
	// GoalEvents, when non-nil, receives the goal run's progress as
	// events: each verdict and failure note as an EventGoal. When nil
	// (the default), verdicts go to Output and failure notes to
	// stderr. See GoalEventObserver and TheoryOfGoalMode.
	GoalEvents GoalEventObserver
	// Generate runs one goal loop: a fresh generation session that
	// re-reads the current filesystem state, with the given loop
	// number, feedback, the previous loops' summaries, and the model
	// the loop should run on (empty keeps the default model).
	Generate GoalLoopGenerator
	// Review reviews the accumulated session diffs after the run.
	Review RunReview
	// ReviewModels lists the models the post-done loops run on, in
	// order. Each done block emitted by a loop selects the next model
	// for the loop that follows; the last model is fixed once reached.
	// When empty, post-done loops keep the default model. See
	// TheoryOfGoalReviewModel.
	ReviewModels []string
}

// GoalEventObserver receives the goal runner's progress events:
// verdicts and failure notes as EventGoal. A display front-end (e.g.,
// tai's TUI) forks this provider to forward the events into its Events
// tab; when nil (the default), the runner writes verdicts to the output
// writer and failure notes to stderr. See TheoryOfGoalMode and
// TheoryOfLoopEvents.
type GoalEventObserver func(Event)

// GoalLoop is the 1-based number of the goal loop that a generation
// session serves. The zero value marks a non-goal run (AnyTextCommand,
// ai, next, ping): the loop's events then carry no loop attribution. The
// goal runner forks each loop's number into the loop's scope, and
// GenerateWithResultWithStats passes it into RunOptions.Loop so the
// loop stamps every event with it. See TheoryOfGoalMode and
// TheoryOfLoopEvents.
type GoalLoop int

// GoalLoopGenerator runs one goal loop. The loop number, the feedback,
// and the summaries of previous loops reach the loop's scope through
// the forks of the goal runner. A non-empty reviewModel forks
// flags.ModelName to the review model, so the loop runs on it; see
// TheoryOfGoalReviewModel.
type GoalLoopGenerator func(
	ctx context.Context,
	loop int,
	feedback GoalFeedback,
	summaries GoalLoopSummaries,
	reviewModel string,
) (
	result Result,
	stats []AttemptStat,
	err error,
)

// GoalResult reports the outcome of a goal run. See TheoryOfGoalMode.
type GoalResult struct {
	// Achieved reports whether the run ended on a loop that emitted a
	// done block while applying no change blocks.
	Achieved bool
	// LoopsRun is the number of loops executed, including any extra
	// verification loop beyond the iteration budget.
	LoopsRun int
	// Stats carries the attempt statistics of every loop, with the Loop
	// field identifying the goal loop that produced each attempt.
	Stats []AttemptStat
}

// GoalRun runs the goal loop mechanism: repeated fresh generation loops
// until a change-free loop emits a done block — the run's only exit —
// the iteration budget is exhausted, or the same error repeats,
// followed by a review of the accumulated diffs. Verdicts and failure
// notes go to output. See TheoryOfGoalMode.
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
// previous loops, the pending done-block declaration, the sticky count of
// done blocks emitted so far (post-done loops run on the review model the
// count selects), repeated-error tracking, and the terminal flags. See
// TheoryOfGoalMode and TheoryOfGoalReviewModel.
type goalLoopState struct {
	feedback                GoalFeedback
	summaries               GoalLoopSummaries
	pendingDoneVerification bool
	doneCount               int
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
	reporter goalReporter,
) bool {
	// A disk-change handoff ends the loop and hands its content to the
	// next loop, which reloads the filesystem and restarts the work.
	// See TheoryOfDiskChangeHandoff.
	if handoff, ok := asDiskChangeHandoff(err); ok {
		return s.applyDiskChangeHandoff(loopsRun, handoff, reporter)
	}
	if err != nil {
		return s.applyLoopError(loopsRun, err, reporter)
	}
	return s.applyLoopSuccess(loopsRun, result, reporter)
}

// applyDiskChangeHandoff folds a loop that ended on a disk-change handoff
// into the runner state: the handoff content — the interrupted work's
// summary plus the disk-change notice — becomes the next loop's feedback,
// and the next loop reloads the filesystem and restarts the work. A disk
// change is an external event, not a model failure, so the
// consecutive-error counter resets and a pending done declaration is
// overturned. See TheoryOfGoalMode and TheoryOfDiskChangeHandoff.
func (s *goalLoopState) applyDiskChangeHandoff(
	loopsRun int,
	err *DiskChangeHandoffError,
	reporter goalReporter,
) bool {
	reporter.message(fmt.Sprintf(
		"\n[Goal Loop %d Ended Early: %v. The next loop reloads the filesystem and retries from scratch.]\n",
		loopsRun, err.Err))

	s.pendingDoneVerification = false
	s.consecutiveErrors = 0
	s.lastErrMsg = ""

	feedback := "[System note: The previous goal loop ended early: " + err.Err.Error() + ". The loop's in-memory file snapshot no longer matched the disk, so no attempt retry could repair it. This loop reloads the current filesystem state and restarts the work from scratch: re-read the affected files and re-apply the intended changes against the fresh snapshot."
	if err.Handoff != nil && strings.TrimSpace(err.Handoff.Prompt) != "" {
		feedback += "\n\nHandoff summary of the interrupted work:\n" + err.Handoff.Prompt
	}
	feedback += "]"
	s.feedback = GoalFeedback(feedback)
	return false
}

// applyLoopError folds a failed loop into the runner state: a failure
// overturns a pending done declaration and carries corrective feedback
// into the next loop; the same error repeated maxConsecutiveGoalErrors
// times in a row stops the run. See TheoryOfGoalMode.
func (s *goalLoopState) applyLoopError(loopsRun int, err error, reporter goalReporter) bool {
	reporter.failure(fmt.Sprintf("Goal loop %d failed: %v\n", loopsRun, err))

	errMsg := err.Error()
	if errMsg == s.lastErrMsg {
		s.consecutiveErrors++
	} else {
		s.consecutiveErrors = 1
		s.lastErrMsg = errMsg
	}
	if s.consecutiveErrors >= maxConsecutiveGoalErrors {
		reporter.failure(fmt.Sprintf(
			"\n[Goal Stopped: the same error occurred %d consecutive times]\n%s\n",
			maxConsecutiveGoalErrors, errMsg))
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
// uncorrected malformed blocks carry re-emit feedback into the next
// loop; a done block from a loop that applied no change blocks achieves
// the goal — the done block is the run's only exit; a loop that applied
// change blocks never ends the run — even when it also emits a done
// block, because the changes must be checked by the next loop — and a
// done declaration with changes carries the verification prompt into
// the next loop; a loop that applied no change blocks without a done
// block is a model output failure, so the runner carries corrective
// feedback into the next loop and continues; a clean loop with changes
// and no done block clears the feedback. See TheoryOfGoalMode and
// TheoryOfGoalReviewModel.
func (s *goalLoopState) applyLoopSuccess(loopsRun int, result Result, reporter goalReporter) bool {
	s.consecutiveErrors = 0
	s.lastErrMsg = ""

	if len(result.ParseErrors) > 0 {
		first := result.ParseErrors[0]
		reporter.failure(fmt.Sprintf(
			"Goal loop %d: %d malformed block(s) could not be corrected (e.g., kind %q boundary %q); some changes may be missing.\n",
			loopsRun, len(result.ParseErrors), first.BlockKind, first.Boundary))
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

	// A change-free loop that emitted the done block is the run's only
	// exit: within one loop the model can chain as many generations as
	// it needs, so an empty diff set means it examined the current
	// state and concluded without corrections. A change-free loop
	// WITHOUT a done block is a model output failure, not a terminal
	// state: the loop produced neither progress nor the completion
	// signal, and the plausible cause is a severe defect in its own
	// conversation history, which cannot be repaired from within it.
	// The next loop starts fresh, so the run continues with corrective
	// feedback; a pending done declaration stays pending, because the
	// silent loop verified nothing. See TheoryOfGoalMode.
	if len(result.Diffs) == 0 {
		if foundDone {
			reporter.message(fmt.Sprintf("\n[Goal Achieved after %d loop(s)]\n", loopsRun))
			s.achieved = true
			return true
		}
		reporter.failure(fmt.Sprintf(
			"Goal loop %d applied no change blocks and emitted no done block (model output failure); continuing with corrective feedback.\n",
			loopsRun))
		s.feedback = GoalFeedback(goalNoDoneFeedbackPrompt)
		return false
	}

	// A loop that applied change blocks never ends the run, even when
	// it also emits a done block: the changes must be verified by the
	// next loop, which re-reads the current filesystem state. The done
	// block emitted here is a declaration the next loop challenges; the
	// declaration switches the following loop to a review model, and
	// each done block emitted here advances the review-model selection
	// by one — the count is sticky, so the loops that carry later
	// corrections stay on the review models too. See TheoryOfGoalMode
	// and TheoryOfGoalReviewModel.
	if foundDone {
		s.pendingDoneVerification = true
		s.doneCount++
		s.feedback = GoalFeedback(goalDoneVerificationPrompt)
		return false
	}

	s.pendingDoneVerification = false
	s.feedback = ""
	return false
}

// goalReporter routes one goal-run progress message to its destination:
// an EventGoal through the goal event observer when one is set — a
// display front-end's Events tab — or the output writer (failure notes
// to stderr) otherwise, preserving the command-line formatting. See
// TheoryOfGoalMode.
type goalReporter struct {
	output   io.Writer
	observer GoalEventObserver
}

// message delivers one progress message: an EventGoal carrying the
// trimmed text through the observer, or the text verbatim to the output
// writer.
func (r goalReporter) message(text string) {
	if r.observer != nil {
		r.observer(Event{Kind: EventGoal, Detail: strings.TrimSpace(text)})
		return
	}
	fmt.Fprint(r.output, text)
}

// failure delivers one failure note: an EventGoal through the observer,
// or the text to stderr as before.
func (r goalReporter) failure(text string) {
	if r.observer != nil {
		r.observer(Event{Kind: EventGoal, Detail: strings.TrimSpace(text)})
		return
	}
	fmt.Fprint(os.Stderr, text)
}

func RunGoal(ctx context.Context, opts GoalOptions) GoalResult {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

	// The reporter routes every progress message: as events through the
	// goal event observer when one is set (a display front-end's Events
	// tab), or written to the output writer (failure notes to stderr)
	// otherwise. See TheoryOfGoalMode.
	reporter := goalReporter{
		output:   opts.Output,
		observer: opts.GoalEvents,
	}

	state := &goalLoopState{}
	var allStats []AttemptStat
	var allDiffs []changes.FileDiff
	loopsRun := 0

	// runOneLoop executes one generation loop and folds its outcome into
	// the runner state. It reports whether the run should stop after the
	// loop. A loop after the first done block runs on a review model when
	// any are configured: each done block emitted by an earlier loop
	// advances the selection by one, and the last model is fixed once
	// reached. See TheoryOfGoalReviewModel.
	runOneLoop := func() bool {
		loopStart := len(allStats)
		reviewModel := ""
		if state.doneCount > 0 && len(opts.ReviewModels) > 0 {
			index := state.doneCount - 1
			if index >= len(opts.ReviewModels) {
				index = len(opts.ReviewModels) - 1
			}
			reviewModel = opts.ReviewModels[index]
		}
		result, stats, err := opts.Generate(ctx, loopsRun, state.feedback, state.summaries, reviewModel)
		allStats = append(allStats, stats...)
		for i := loopStart; i < len(allStats); i++ {
			allStats[i].Loop = loopsRun
		}
		state.summaries = appendLoopSummaries(state.summaries, loopsRun, allStats[loopStart:])
		allDiffs = append(allDiffs, result.Diffs...)
		return state.applyLoopResult(loopsRun, result, err, reporter)
	}

	for loopsRun < maxGoalIterations {
		loopsRun++
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
		runOneLoop()
	}

	if !state.achieved && !state.stopRequested {
		reporter.message(fmt.Sprintf("\n[Goal Not Achieved after %d loops]\n", loopsRun))
	}

	if err := opts.Review(ctx, os.Stdout, allDiffs); err != nil {
		reporter.failure(fmt.Sprintf("Review failed: %v\n", err))
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
// opens a fresh scope via reset, forks the loop's number, the loop's
// feedback, the previous loops' summaries, and — when the runner requests
// the review model — flags.ModelName into it, and resolves
// GenerateWithResultWithStats from the forked scope, so the loop reads the
// latest filesystem state, sees the previous loops' feedback and
// summaries in its system prompt, stamps its events with its loop
// number, and runs on the requested model. Generation streams to
// os.Stdout: in TUI mode the terminal's stdout is the null device and
// the display captures output through state decorators, so a per-loop
// writer would duplicate output. See TheoryOfGoalMode and
// TheoryOfGoalReviewModel.
func makeGoalLoopGenerator(reset dscope.Reset) GoalLoopGenerator {
	return func(ctx context.Context, loop int, feedback GoalFeedback, summaries GoalLoopSummaries, reviewModel string) (Result, []AttemptStat, error) {
		scope := reset()
		scope = scope.Fork(func() GoalLoop { return GoalLoop(loop) })
		if feedback != "" {
			scope = scope.Fork(func() GoalFeedback { return feedback })
		}
		if len(summaries) > 0 {
			scope = scope.Fork(func() GoalLoopSummaries { return summaries })
		}
		if reviewModel != "" {
			scope = scope.Fork(func() flags.ModelName { return flags.ModelName(reviewModel) })
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
// goal system prompt) apply to every loop. The goal event observer is
// resolved from the scope, so a display front-end's fork receives the
// goal verdicts as events. The configured review models rotate across
// the post-done loops, one per done block; see TheoryOfGoalReviewModel.
func (Module) GoalRun(
	reset dscope.Reset,
	runReview RunReview,
	observeGoal GoalEventObserver,
	reviewModels ReviewModels,
) GoalRun {
	models := make([]string, 0, len(reviewModels))
	for _, model := range reviewModels {
		if model != "" {
			models = append(models, model)
		}
	}
	return func(ctx context.Context, output io.Writer) GoalResult {
		return RunGoal(ctx, GoalOptions{
			Output:       output,
			Generate:     makeGoalLoopGenerator(reset),
			Review:       runReview,
			GoalEvents:   observeGoal,
			ReviewModels: models,
		})
	}
}
