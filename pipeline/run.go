package pipeline

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/records"
)

const TheoryOfContextPhilosophy = `
The system provides all context the model needs in a single generation
request, not through multi-turn conversation. This single-shot approach sets
it apart from agentic agents that grow context via dialogue.

Upfront construction: file contents, dependency graphs, system prompts, and
task instructions are assembled before the first call. Pruning removes
irrelevant files; simplification strips function bodies and comments from
non-focus packages; token budgeting caps input size. The model reasons over
the complete picture into changes ready for human review.

Architectural constraints:

- No long conversations. The system accumulates no dialogue across tasks;
  each invocation builds fresh context from the filesystem state. The ai
  command's interactive mode lets the user type messages across turns, but
  each turn sends the full accumulated context, not a compressed fragment.

- No conversation compression. Old dialogue is never summarized to free
  token budget; context is managed solely by pruning, AST-level
  simplification, and deterministic file ordering. Handoff
  (TheoryOfHandoff) condenses truncated output for one-shot error recovery,
  not persistent history. Thought summarization (TheoryOfThoughtsSummarize)
  writes to the user's screen for readability; it never feeds back as
  compressed context.

- No blind exploration. The upfront context always carries the complete
  declaration surface, so the model never starts from nothing (see
  gotools.TheoryOfContextStrategy). Implementation source is fetched on
  demand with go-src blocks — a targeted pull from the known surface, not
  semantic-search probing. Ingest blocks serve external resources unavailable
  at construction time (network fetches, glob expansion), not as a
  substitute for upfront context.

- Multi-round generation is task decomposition, not conversation. Continue
  blocks split large tasks into bounded rounds; shell and go-test blocks run
  autonomous verification. The loop executes tasks; it is not a chatbot.

Features assuming a long-conversation model — dialogue-grown context, turns
summarized to free budget, conversation history as knowledge base — violate
this philosophy and are out of scope.
`

const TheoryOfLoops = `
The pipeline unifies the generation loop pattern across all generation
commands (go, any, ai, next). The core pattern:
1. Wrap state with ParserState to collect blocks during streaming
2. Execute the phase chain until done
3. Unwrap ParserState to get the final state and collected blocks
4. Process collected blocks through components (if any)
5. Repeat until no components trigger or MaxRounds is reached

Run is exposed as an event iterator: func(ctx, opts, result *Result)
iter.Seq2[Event, error]. The result is filled incrementally as the run
progresses; every notable occurrence — round lifecycle (start, success,
truncation, error), retry decisions and handoffs, synthesized completion
summaries, per-round token usage, component-triggered and idle
continuations — is yielded as an Event as it occurs (see
TheoryOfLoopEvents). The terminal error, if any, arrives with the final
yield's error component when the run stops. Callers may suspend and resume
the run via iter.Pull2, inspecting the result between pulls.

A round is one pass through the phase chain, producing a summary and parts.
The round's outcome is captured by roundResult: the updated state, the
round's summary, and the parts that determine whether the next round
starts. The parts are the round's return value: when any exist, they are
appended to the state as user content and the next round begins. The
summary and the parts are both determined before the round ends: the
summary by the model's summary blocks (or synthesis on truncation), and the
parts by ProcessComponents. The round logic lives in loopState.runRound;
the main loop in Run simply executes rounds and continues while
roundResult.continueNext is true. A retry is a re-execution of the phase
chain within the same round, triggered by a missing completion (no summary
block) or an error after content output. Retries count as loops in round
statistics.

Retry on missing completion and handoff: a round without a summary block —
whether the generation limit truncated the model mid-stream before its
closing summary block, the model emitted a summary and continued until cut
off, or the model ended its response without one — or with an abnormal
finish reason (e.g., "length" from max-token truncation), is retried from
the original pre-generation State. Truncation often happens because the
model attempted too many changes in a single turn. When output meets the
minimum threshold, the handoff process creates a
self-contained summary carrying forward established conclusions, attempted
changes, and task-partitioning guidance. The retry user prompt explicitly
instructs the model to partition extensive modifications: implement an initial
manageable subset of changes in the current round, end with a summary block, and
use a continue block to carry over the remaining work into subsequent rounds,
preventing repeated truncation loops. Short or empty outputs are retried directly.
See TheoryOfHandoff.

The summary block is non-negotiable: every round that ends without a summary
block is retried when RetryOnMissingCompletion is enabled, including rounds
whose blocks trigger components (ingest, shell, continue, go-test, go-src). No
block kind replaces or implies the summary. The retry feedback names the
violation — missingSummaryRetryPrefix when the response simply ended without
the summary block, incompleteOutputHandoffPrefix when the finish reason shows
truncation — states the attempt number, and instructs the model to re-emit
every block it intends to take effect together with the summary block,
because the failed attempt's blocks were discarded.

When the retry budget is exhausted and the final attempt still lacks a summary
block, the loop synthesizes a summary from the round's output and appends it
to the state as a summary block, so the round has a completion signal for the round
statistics and the TUI's Events tab. The synthesis applies to every exhausted
round, including rounds whose blocks trigger components.

Retry on error: an error after content output retries from the state that includes
the partial output, appending the error context and the handoff summary as user content.
Errors before any content output do not retry.

Retry feedback states the current attempt number (e.g., "retry attempt 1 of 3") so
the model knows how much budget remains and can prioritize correcting the error.
`

const TheoryOfUsageLogging = `
The token usage of each generation round is recorded by the Run loop itself,
not by individual commands. After each round, the usage record carries the
1-based round number and the prompt, cached, completion, and thought token
counts from the round's final usage. The record flows to the run's event
stream as an EventUsage — the single display source for a live consumer;
the TUI renders its "[Usage]" line from the event — and to a "usage" log
entry, so every generation command — go, any, ai, next, ping — shows token
consumption in its logs and in the TUI's Logs pane. A round that ends with
an error carries an outcome marker ("error" in the log entry, in the
event's Detail, and in the rendered line's "(error)" suffix), so token
consumption is traceable for every attempt, including retries. Rounds that
record no token usage emit nothing.

The usage is extracted by scanning the state's contents appended since the
start of the round and taking the final Usage part, rather than summing
intermediate usage snapshots that may be emitted by streaming providers
(e.g., Gemini's streaming UsageMetadata).
`

const errorRetryPrefix = "[System note: An error occurred: %s. This is retry attempt %d of %d. The failed attempt's output was discarded — its structured blocks were NOT applied. If the intended modifications are extensive, partition the work across multiple rounds using continue blocks rather than emitting all changes at once. Re-emit every block you intend to take effect, then correct the issue and continue.]\n\n"

const defaultMaxRetries = 3

// maxParseErrorRounds bounds the number of rounds that feed parse errors
// back to the model for self-correction. The bound is cumulative per run:
// it resets only when a round produces no parse errors, so a model that
// persistently emits malformed blocks cannot restart the correction cycle
// indefinitely when other components keep triggering rounds. When the
// bound is reached, feedback stops and the uncorrected parse errors are
// recorded in Result.ParseErrors. See TheoryOfLoops.
const maxParseErrorRounds = 3

const incompleteOutputHandoffPrefix = "[System note: The previous generation was truncated before completion. This is retry attempt %d of %d. The truncated output was discarded and will not appear in history — its structured blocks were NOT applied. Truncation typically occurs when attempting too many changes in a single response, exceeding the output limit. If the planned modifications are extensive, do NOT attempt to emit all changes at once. Instead, partition the work: implement a manageable initial subset of change blocks in this round, and use a continue block to carry over the remaining tasks into subsequent rounds. Re-emit every block you intend to take effect in this round. Nothing in the interrupted attempt was completed: changes are atomic, so there is no completed work on disk, and no next step to carry forward without implementation. Below is the self-contained handoff summary from the previous attempt, preserving its valuable thinking: discoveries, insights, analysis, decisions, and attempted changes. Use it as reference to partition and guide your work, but continue to think for yourself: the handoff does not replace your own reasoning, and you must still analyze the problem and decide how to proceed.]\n\n"

const missingSummaryRetryPrefix = "[System note: Your previous response ended WITHOUT the required summary block. This is retry attempt %d of %d. The summary block is MANDATORY in every response: it is the completion signal the system uses to verify that generation ended normally and followed the rules. No other block — change, shell, go-test, go-src, ingest, continue — replaces or implies it. The previous attempt was discarded: its structured blocks were NOT processed. Re-emit every block you intend to take effect, then close the response with a summary block (a \"- \" bullet list of what was done; \"No changes were needed.\" when nothing was done). Never end a response on any block other than the summary block.]\n\n"

// StateDecorator wraps a generation state before the loop starts,
// returning a new state that observes or modifies the original. The
// decorator is applied after interaction recording, so it sees every
// subsequent content append. Multiple decorators are applied in order,
// each wrapping the state produced by the previous one. See
// RunOptions.StateDecorators.
type StateDecorator func(generators.State) generators.State

// InteractionRecorder provider: the default is nil, meaning no interaction
// recording. Commands that want recording pass their recorder explicitly
// through RunOptions.InteractionRecorder, which takes precedence over the
// loop's default. Keeping the default provider here (rather than in an
// outer module) avoids duplicate-definition conflicts in dscope scopes.
// See records.TheoryOfInteractionRecording.
func (Module) InteractionRecorder() InteractionRecorder {
	return nil
}

// The records.Recorder implements InteractionRecorder. The assertion lives
// here rather than in records: pipeline imports records, and the reverse
// import would create a cycle. See records.TheoryOfInteractionRecording.
var _ InteractionRecorder = (*records.Recorder)(nil)

// Run executes generation rounds in a loop. Each round wraps the state
// with ParserState, executes the phase chain, processes blocks via
// components, and continues if a component triggers a new round.
// When Components is empty, the loop runs a single round (single-shot
// mode). The result is filled into result as the run progresses, and
// the returned iterator yields one Event per notable occurrence —
// round lifecycle (start, success, truncation, error), retries and
// handoffs, synthesized completion summaries, attempt finish reasons,
// per-round token usage, periodic thought summaries, and
// component-triggered or idle continuations — with the terminal
// error, if any, arriving with the final yield's error component.
// Callers may suspend and resume the run via iter.Pull2, inspecting
// the result between pulls. See TheoryOfLoops and TheoryOfLoopEvents.
type Run func(ctx context.Context, opts RunOptions, result *Result) iter.Seq2[Event, error]

// roundResult is the outcome of one generation round: the updated state,
// the round's summary, and the parts that determine whether the next
// round starts. The parts are the round's return value: when
// continueNext is true, they are appended to the state as user content
// and the next round begins. See TheoryOfLoops.
type roundResult struct {
	state        generators.State
	summaries    []string
	parts        []generators.Part
	continueNext bool
	// finalBlocks is set when the round is the whole run (single-shot
	// mode): the loop ends with these blocks as the result.
	finalBlocks []blocks.Block
}

// loopState holds the mutable state of a generation loop run. The main loop
// in Run executes rounds via runRound; the state here is updated by each round
// and carried into the next. Events are yielded through the guarded
// emitEvent/emitTerminal methods: after the consumer stops, further events
// are dropped so the loop's bookkeeping can still complete. See
// TheoryOfLoops and TheoryOfLoopEvents.
type loopState struct {
	ctx     context.Context
	opts    RunOptions
	rec     InteractionRecorder
	result  *Result
	yield   func(Event, error) bool
	stopped bool
	state   generators.State

	// round is the 1-based round number of the round being executed.
	// Retries within a round share the round number; their events carry
	// the attempt number separately. See TheoryOfLoopEvents.
	round int

	remainingBlocks []blocks.Block
	maxRetries      int

	parseErrorCorrectionRounds int
	uncorrectedParseErrors     []*blocks.BlockParseError
	skipOnRoundStart           bool

	runErr error

	// logger records the aggregated token usage of each round as a
	// "usage" log entry; the event stream (EventUsage) is the display
	// source for a live consumer such as the TUI's Events tab. The
	// logger is dscope provided, captured by the Run provider. See
	// TheoryOfUsageLogging.
	logger logs.Logger
}

func (ls *loopState) runRound() (roundResult, error) {
	// Round start: report to the interaction recorder, reset per-round
	// state (e.g., MemoryStore.Reset), and open the round in the event
	// stream. See TheoryOfLoopEvents.
	if ls.rec != nil && ls.rec.Enabled() {
		ls.rec.RoundStart()
	}
	ls.emitEvent(Event{Kind: EventRoundStart, Round: ls.round})
	if ls.opts.OnRoundStart != nil && !ls.skipOnRoundStart {
		ls.opts.OnRoundStart()
	}

	var collectedBlocks []blocks.Block
	var roundSummaries []string
	var roundParseErrors []*blocks.BlockParseError
	phaseState := ls.state
	var roundErr error

	// Inner retry loop for missing completion and errors with content output.
	for retry := 0; ; retry++ {
		collectedBlocks = nil
		roundParseErrors = nil

		// Create parser handler that collects blocks and
		// optionally invokes the caller's BlockHandler.
		parserHandler := func(block blocks.Block) error {
			// Report every parsed block to the interaction
			// recorder, whether or not it is consumed by
			// the caller's BlockHandler.
			if ls.rec != nil && ls.rec.Enabled() {
				ls.rec.Block(block)
			}
			if ls.opts.BlockHandler != nil {
				consumed, err := ls.opts.BlockHandler(block)
				if err != nil {
					return err
				}
				if consumed {
					return nil
				}
			}
			collectedBlocks = append(collectedBlocks, block)
			return nil
		}

		// Wrap state with ParserState.
		parserState := blocks.NewParserState(ls.state, parserHandler)
		wrappedState := generators.State(parserState)

		// Build and execute phase chain.
		phase := ls.opts.PhaseBuilder(ls.opts.Generator)
		for phase != nil {
			var err error
			phase, wrappedState, err = phase(ls.ctx, wrappedState)
			if err != nil {
				roundErr = err
				break
			}
		}

		// Unwrap ParserState to get the base state. A phase may
		// return a nil state on error; fall back to the pre-phase
		// state so OnPhaseError receives a valid state rather
		// than a nil pointer that would cause a panic.
		if wrappedState == nil {
			phaseState = ls.state
		} else if ps, ok := generators.As[*blocks.ParserState](wrappedState); ok {
			phaseState = ps.Unwrap()
			// Collect parse errors from the stream so they can be
			// fed back to the model for self-correction.
			// See TheoryOfParseErrorCollection.
			roundParseErrors = ps.ParseErrors()
			// Report malformed blocks to the interaction recorder.
			if ls.rec != nil && ls.rec.Enabled() {
				for _, parseErr := range roundParseErrors {
					ls.rec.ParseError(parseErr)
				}
			}
		} else {
			phaseState = wrappedState
		}

		// The attempt's finish reason feeds both the event stream —
		// every attempt's completion signal, including attempts that
		// later fail — and the completion check below. The extraction
		// window is exactly this attempt's contents: ls.state still
		// holds the pre-attempt base on both the error-retry and
		// truncation-retry paths. See TheoryOfLoopEvents.
		finishReason := extractFinishReason(phaseState, generators.CountContents(ls.state))
		if finishReason != "" {
			ls.emitEvent(Event{Kind: EventFinish, Round: ls.round, Detail: finishReason})
		}

		if roundErr != nil {
			// Retry on any error when content was output during
			// the round. The loop summarizes the incomplete
			// output, appends both the error context and the
			// summary as user content so the model can correct
			// its output, resets per-round state via
			// OnRoundStart (which resets the MemoryStore,
			// discarding failed changes), and retries from the
			// updated state. Errors that occur before any
			// content is output do not trigger retry. The
			// feedback states the current attempt number so
			// the model knows how much retry budget remains.
			// See TheoryOfLoops.
			if ls.opts.RetryOnError && retry < ls.maxRetries {
				prevCount := generators.CountContents(ls.state)
				if generators.CountContents(phaseState) > prevCount {
					ls.state = phaseState

					// Report the failed attempt to the
					// interaction recorder.
					if ls.rec != nil && ls.rec.Enabled() {
						ls.rec.RoundError(roundErr)
						ls.rec.Event("decision", fmt.Sprintf("error after partial output triggered retry: attempt %d/%d: %v", retry+1, ls.maxRetries, roundErr))
					}

					// Report the retry decision to the event stream.
					// See TheoryOfLoopEvents.
					ls.emitEvent(Event{
						Kind:        EventRetry,
						Round:       ls.round,
						Attempt:     retry + 1,
						MaxAttempts: ls.maxRetries,
						Err:         roundErr,
						Detail:      "error after partial output",
					})

					var retryParts []generators.Part
					retryParts = append(retryParts, generators.Text(
						fmt.Sprintf(errorRetryPrefix, roundErr.Error(), retry+1, ls.maxRetries)))

					// For change block apply errors, add specific
					// guidance: the retry discards ALL change
					// blocks from the failed attempt (OnRoundStart
					// resets the in-memory store below), so the
					// model must re-emit every intended change
					// block, correcting the one that failed.
					// See TheoryOfLoops.
					var applyErr *changes.ApplyError
					if errors.As(roundErr, &applyErr) {
						retryParts = append(retryParts, generators.Text(
							"\nThe change block that caused the error was NOT applied, and this retry discards ALL change blocks from the failed attempt. Re-emit every intended change block, correcting the one that caused the error.\n"))
					}

					summary := ""
					retryPrompt := ""
					if ls.opts.Handoff != nil {
						incompleteText := ExtractIncompleteOutput(phaseState, prevCount)
						if incompleteText != "" {
							handoff, handoffErr := ls.opts.Handoff(incompleteText)
							if handoffErr == nil && handoff != nil {
								summary = handoff.Summary
								retryPrompt = handoff.Prompt
								// Report the produced handoff to the
								// event stream. See TheoryOfLoopEvents.
								ls.emitEvent(Event{
									Kind:        EventHandoff,
									Round:       ls.round,
									Attempt:     retry + 1,
									MaxAttempts: ls.maxRetries,
									Summary:     handoff.Summary,
									Handoff:     handoff,
								})
								// Account the handoff request's own token
								// spend before the failed round is recorded.
								// The window starts at the failed attempt's
								// base, so usage retained from earlier error
								// retries is never re-attributed to this
								// attempt. See TheoryOfHandoffUsageAccounting.
								phaseState = appendHandoffUsage(phaseState, prevCount, handoff.Usage)
							}
						}
					}

					// Record the failed round in round statistics
					// so it appears as a separate loop.
					// See TheoryOfRoundStatistics.
					if ls.opts.OnRoundTruncated != nil {
						if rerr := ls.opts.OnRoundTruncated(phaseState, ls.state, summary); rerr != nil {
							roundErr = rerr
							break
						}
					}

					// Append the handoff prompt as the retry user input.
					if retryPrompt != "" {
						retryParts = append(retryParts, generators.Text(
							formatHandoffPrompt(retryPrompt, retry+1, ls.maxRetries)))
					}

					var appendErr error
					ls.state, appendErr = ls.state.AppendContent(&generators.Content{
						Role:  generators.RoleUser,
						Parts: retryParts,
					})
					if appendErr != nil {
						break
					}
					roundErr = nil
					if ls.opts.OnRoundStart != nil {
						ls.opts.OnRoundStart()
					}
					continue
				}
			}
			break
		}

		// Always extract and remove summary blocks from
		// collectedBlocks. Summaries must be available to
		// OnRoundSuccess regardless of whether retry is
		// enabled. See TheoryOfLoops.
		roundSummaries = nil
		var remaining []blocks.Block
		for _, block := range collectedBlocks {
			if block.Kind == "summary" {
				roundSummaries = append(roundSummaries, block.Body)
			} else {
				remaining = append(remaining, block)
			}
		}
		collectedBlocks = remaining

		// If retry is disabled, we're done with this round.
		if !ls.opts.RetryOnMissingCompletion {
			break
		}

		// Check for completion: the summary block is the only
		// completion signal — no other block kind (ingest, shell,
		// continue, go-test, go-src) completes a round — and an
		// abnormal finish reason (e.g., "length" from max-token
		// truncation) overrides the summary signal and triggers
		// retry. See TheoryOfSummaryCompletionRetry in generate.go.
		hasCompletion := len(roundSummaries) > 0
		isAbnormalFinish := isAbnormalFinishReason(finishReason)

		if hasCompletion && !isAbnormalFinish {
			break
		}
		if retry >= ls.maxRetries {
			break
		}

		// Report the truncated attempt to the interaction
		// recorder.
		if ls.rec != nil && ls.rec.Enabled() {
			ls.rec.RoundTruncated()
			if isAbnormalFinish {
				ls.rec.Event("decision", fmt.Sprintf("abnormal finish reason %q triggered retry: attempt %d/%d", finishReason, retry+1, ls.maxRetries))
			} else {
				ls.rec.Event("decision", fmt.Sprintf("missing completion (no summary block) triggered retry: attempt %d/%d", retry+1, ls.maxRetries))
			}
		}

		// Perform handoff summary on incomplete output if threshold met.
		// attemptBase is both the incomplete-output window and the
		// usage-injection window: the injection sums this attempt's
		// own last usage with the handoff spend, never a prior
		// attempt's. See TheoryOfHandoffUsageAccounting.
		summary := ""
		retryPrompt := ""
		attemptBase := generators.CountContents(ls.state)
		if ls.opts.Handoff != nil {
			incompleteText := ExtractIncompleteOutput(phaseState, attemptBase)
			if incompleteText != "" {
				handoff, rerr := ls.opts.Handoff(incompleteText)
				if rerr == nil && handoff != nil {
					summary = handoff.Summary
					retryPrompt = handoff.Prompt
					// Report the produced handoff to the event
					// stream. See TheoryOfLoopEvents.
					ls.emitEvent(Event{
						Kind:        EventHandoff,
						Round:       ls.round,
						Attempt:     retry + 1,
						MaxAttempts: ls.maxRetries,
						Summary:     handoff.Summary,
						Handoff:     handoff,
					})
					phaseState = appendHandoffUsage(phaseState, attemptBase, handoff.Usage)
				}
			}
		}

		// Report the truncated attempt to the event stream. See
		// TheoryOfLoopEvents. The handoff summary is not repeated
		// here: EventHandoff already carried it.
		truncatedDetail := "missing completion (no summary block)"
		if isAbnormalFinish {
			truncatedDetail = fmt.Sprintf("abnormal finish reason %q", finishReason)
		}
		ls.emitEvent(Event{
			Kind:        EventRoundTruncated,
			Round:       ls.round,
			Attempt:     retry + 1,
			MaxAttempts: ls.maxRetries,
			Detail:      truncatedDetail,
		})

		// Record the truncated round in round statistics.
		if ls.opts.OnRoundTruncated != nil {
			if rerr := ls.opts.OnRoundTruncated(phaseState, ls.state, summary); rerr != nil {
				roundErr = rerr
				break
			}
		}

		// Append the retry feedback. The feedback always names the
		// reason and the attempt number: an abnormal finish reason
		// frames the retry as truncation; any other missing-summary
		// round is a rule violation — the model ended its response
		// without the mandatory summary block — so the feedback says
		// so explicitly. Blocks from the failed attempt were
		// discarded, so the model must re-emit every block it intends
		// to take effect, together with the summary block. The
		// handoff prompt, when one was produced, follows the prefix.
		// See TheoryOfLoops and TheoryOfSummaryCompletionRetry.
		prefixTemplate := incompleteOutputHandoffPrefix
		if !isAbnormalFinish {
			prefixTemplate = missingSummaryRetryPrefix
		}
		retryParts := []generators.Part{
			generators.Text(fmt.Sprintf(prefixTemplate, retry+1, ls.maxRetries)),
		}
		if retryPrompt != "" {
			retryParts = append(retryParts, generators.Text(retryPrompt))
		}
		var appendErr error
		ls.state, appendErr = ls.state.AppendContent(&generators.Content{
			Role:  generators.RoleUser,
			Parts: retryParts,
		})
		if appendErr != nil {
			break
		}

		// Reset for retry.
		if ls.opts.OnRoundStart != nil {
			ls.opts.OnRoundStart()
		}
	}

	if roundErr != nil {
		ls.recordRoundError(roundErr)
		if ls.opts.OnPhaseError != nil {
			phaseState = ls.opts.OnPhaseError(phaseState, roundErr)
		}
		return roundResult{state: phaseState}, roundErr
	}

	// When the retry budget is exhausted and the final attempt still
	// produced no summary block, synthesize a summary from the round's
	// output and append it to the state as a summary block. The
	// synthesis applies to every exhausted round — including rounds
	// whose blocks trigger components — because the summary block is
	// mandatory in every response: the round statistics and the TUI's
	// Events tab need the completion signal. See TheoryOfLoops.
	if len(roundSummaries) == 0 && ls.opts.Handoff != nil {
		attemptBase := generators.CountContents(ls.state)
		incompleteText := ExtractIncompleteOutput(phaseState, attemptBase)
		if incompleteText != "" {
			if handoff, serr := ls.opts.Handoff(incompleteText); serr == nil && handoff != nil {
				// Report the synthesized completion summary to the
				// event stream. See TheoryOfLoopEvents.
				ls.emitEvent(Event{
					Kind:    EventSynthesizedSummary,
					Round:   ls.round,
					Summary: handoff.Summary,
					Handoff: handoff,
				})
				// Account the handoff request's own token spend so the
				// synthesized completion's round statistics and usage
				// line include it. The window starts at the final
				// attempt's base. See TheoryOfHandoffUsageAccounting.
				phaseState = appendHandoffUsage(phaseState, attemptBase, handoff.Usage)
				var appendErr error
				phaseState, appendErr = phaseState.AppendContent(&generators.Content{
					Role: generators.RoleLog,
					Parts: []generators.Part{
						generators.Text(FormatSummaryBlock(handoff.Summary)),
					},
				})
				if appendErr != nil {
					ls.recordRoundError(appendErr)
					if ls.opts.OnPhaseError != nil {
						phaseState = ls.opts.OnPhaseError(phaseState, appendErr)
					}
					return roundResult{state: phaseState}, appendErr
				}
				roundSummaries = append(roundSummaries, handoff.Summary)
			}
		}
	}

	// OnRoundSuccess hook.
	if ls.opts.OnRoundSuccess != nil {
		if serr := ls.opts.OnRoundSuccess(phaseState, roundSummaries); serr != nil {
			ls.recordRoundError(serr)
			return roundResult{state: phaseState}, serr
		}
	}

	// Report the successfully completed round to the interaction
	// recorder and the event stream.
	if ls.rec != nil && ls.rec.Enabled() {
		ls.rec.RoundSuccess(roundSummaries)
	}
	ls.emitEvent(Event{
		Kind:      EventRoundSuccess,
		Round:     ls.round,
		Summary:   strings.Join(roundSummaries, "\n"),
		Summaries: roundSummaries,
	})

	ls.state = phaseState

	// Parse error handling.
	var parseErrorParts []generators.Part
	var roundUncorrected []*blocks.BlockParseError
	parseErrorParts, ls.parseErrorCorrectionRounds, ls.skipOnRoundStart, roundUncorrected =
		decideParseErrorFeedback(roundParseErrors, ls.parseErrorCorrectionRounds)
	if len(roundUncorrected) > 0 {
		ls.uncorrectedParseErrors = appendUncorrectedParseErrors(ls.uncorrectedParseErrors, roundUncorrected)
	}
	if ls.rec != nil && ls.rec.Enabled() {
		if len(parseErrorParts) > 0 {
			ls.rec.Event("decision", fmt.Sprintf("parse error correction attempt %d/%d: %d malformed block(s) fed back to the model", ls.parseErrorCorrectionRounds, maxParseErrorRounds, len(roundParseErrors)))
		} else if len(roundUncorrected) > 0 {
			ls.rec.Event("decision", fmt.Sprintf("parse error correction budget exhausted: %d malformed block(s) recorded as uncorrected", len(roundUncorrected)))
		}
	}

	// Single-shot mode: no component processing.
	if len(ls.opts.Components) == 0 {
		if len(parseErrorParts) > 0 {
			var aerr error
			ls.state, aerr = ls.state.AppendContent(&generators.Content{
				Role:  generators.RoleUser,
				Parts: parseErrorParts,
			})
			if aerr != nil {
				ls.recordRoundError(aerr)
				return roundResult{state: ls.state}, aerr
			}
			ls.emitEvent(Event{
				Kind:   EventComponentsTriggered,
				Round:  ls.round,
				Detail: fmt.Sprintf("parse error feedback: %d user part(s) scheduled the next round", len(parseErrorParts)),
			})
			return roundResult{
				state:        ls.state,
				summaries:    roundSummaries,
				continueNext: true,
			}, nil
		}
		return roundResult{
			state:       ls.state,
			summaries:   roundSummaries,
			finalBlocks: collectedBlocks,
		}, nil
	}

	// Process components.
	var roundRemaining []blocks.Block
	var combinedParts []generators.Part
	var triggered bool
	var cerr error
	roundRemaining, ls.state, combinedParts, triggered, cerr = components.ProcessComponents(
		ls.ctx, ls.opts.Components, collectedBlocks, ls.state,
		ls.opts.Root, ls.opts.HTTPClient,
	)
	if cerr != nil {
		ls.recordRoundError(cerr)
		return roundResult{state: ls.state}, cerr
	}
	ls.remainingBlocks = append(ls.remainingBlocks, roundRemaining...)

	if len(parseErrorParts) > 0 {
		combinedParts = append(parseErrorParts, combinedParts...)
		triggered = true
	}

	if triggered {
		if len(combinedParts) > 0 {
			var aerr error
			ls.state, aerr = ls.state.AppendContent(&generators.Content{
				Role:  generators.RoleUser,
				Parts: combinedParts,
			})
			if aerr != nil {
				ls.recordRoundError(aerr)
				return roundResult{state: ls.state}, aerr
			}
		}
		if ls.rec != nil && ls.rec.Enabled() {
			ls.rec.Event("decision", fmt.Sprintf("components triggered a new generation round: %d user part(s)", len(combinedParts)))
		}
		ls.emitEvent(Event{
			Kind:   EventComponentsTriggered,
			Round:  ls.round,
			Detail: fmt.Sprintf("%d user part(s) scheduled the next round", len(combinedParts)),
		})
		return roundResult{
			state:        ls.state,
			summaries:    roundSummaries,
			parts:        combinedParts,
			continueNext: true,
		}, nil
	}

	if ls.opts.OnIdle != nil {
		var idleContinue bool
		ls.state, idleContinue, cerr = ls.opts.OnIdle(ls.ctx, ls.state)
		if cerr != nil {
			ls.recordRoundError(cerr)
			return roundResult{state: ls.state}, cerr
		}
		if idleContinue {
			if ls.rec != nil && ls.rec.Enabled() {
				ls.rec.Event("decision", "idle handler returned user input; starting a new round")
			}
			ls.emitEvent(Event{Kind: EventIdle, Round: ls.round})
			return roundResult{
				state:        ls.state,
				summaries:    roundSummaries,
				continueNext: true,
			}, nil
		}
	}

	return roundResult{
		state:     ls.state,
		summaries: roundSummaries,
	}, nil
}

// recordRoundUsage records the aggregated token usage of one round: to
// the run's event stream as an EventUsage (the display source for a live
// consumer) and as a "usage" log entry. Rounds that record no token
// usage emit nothing. See TheoryOfUsageLogging and TheoryOfLoopEvents.
func (ls *loopState) recordRoundUsage(state generators.State, roundBaseCount int, roundNumber int, outcome string) {
	// The usage is the last Usage part among the contents appended since
	// the round started, not a sum of streaming snapshots.
	// See TheoryOfUsageLogging.
	usage := extractLastUsage(state, roundBaseCount)
	if usage.Prompt.TokenCount == 0 &&
		usage.Prompt.TokenCountCached == 0 &&
		usage.Candidates.TokenCount == 0 &&
		usage.Thoughts.TokenCount == 0 {
		return
	}
	ls.emitEvent(Event{
		Kind:   EventUsage,
		Round:  roundNumber,
		Usage:  usage,
		Detail: outcome,
	})
	args := []any{
		"round", roundNumber,
		"prompt", usage.Prompt.TokenCount,
		"cached", usage.Prompt.TokenCountCached,
		"completion", usage.Candidates.TokenCount,
		"thoughts", usage.Thoughts.TokenCount,
	}
	if outcome != "" {
		args = append([]any{"outcome", outcome}, args...)
	}
	ls.logger.InfoContext(ls.ctx, "usage", args...)
}

// recordRoundError reports a failed round to the interaction recorder
// when recording is active.
func (ls *loopState) recordRoundError(err error) {
	if ls.rec != nil && ls.rec.Enabled() {
		ls.rec.RoundError(err)
	}
}

// finishWithError fills the result with the final state and yields the
// terminal error event, ending the run. The caller must return
// immediately after the call.
func (ls *loopState) finishWithError(err error, finalState generators.State) {
	ls.result.FinalState = finalState
	ls.result.RemainingBlocks = ls.remainingBlocks
	ls.result.ParseErrors = ls.uncorrectedParseErrors
	ls.runErr = err
	ls.emitTerminal(Event{
		Kind:  EventRoundError,
		Round: ls.round,
		Err:   err,
	}, err)
}

// finish fills the result with the final state and ends the run without
// an error.
func (ls *loopState) finish(finalState generators.State, finalBlocks []blocks.Block) {
	ls.result.FinalState = finalState
	ls.result.RemainingBlocks = finalBlocks
	ls.result.ParseErrors = ls.uncorrectedParseErrors
}

// BlockHandler processes a block during streaming. If consumed is true,
// the block is not passed to ProcessComponents. If err is non-nil,
// streaming stops immediately. See TheoryOfLoops.
type BlockHandler func(block blocks.Block) (consumed bool, err error)

type InteractionRecorder interface {
	// Enabled reports whether recording is active. When false, the loop
	// does not wrap the state, record contents, or call the lifecycle
	// methods.
	Enabled() bool
	// StartSession begins a recording session for the given command.
	// Called once when the loop starts.
	StartSession(command string)
	// EndSession closes the current session with the given outcome.
	// A non-nil error marks the session as failed.
	EndSession(err error)
	// SystemPrompt records the session's system prompt. Called once when
	// the loop starts.
	SystemPrompt(prompt string)
	// RoundStart marks the beginning of a generation round.
	RoundStart()
	// RoundSuccess marks a round that completed normally, carrying the
	// summary block bodies.
	RoundSuccess(summaries []string)
	// RoundTruncated marks a round that ended without a completion signal
	// (no summary block or abnormal finish reason) and was retried.
	RoundTruncated()
	// RoundError marks a round that failed with an error.
	RoundError(err error)
	// Content records a content appended to the generation state.
	Content(content *generators.Content)
	// Block records a structured block parsed from the model output.
	Block(block blocks.Block)
	// ParseError records a malformed block that could not be parsed.
	ParseError(parseErr *blocks.BlockParseError)
	// Event records an arbitrary session event with the current round
	// number, carrying a type and a free-form detail. The generation loop
	// uses it for flow decisions (retries, parse-error corrections,
	// component-triggered rounds, session metadata), and generator
	// implementations use it through generators.EventRecorderFromContext
	// for API-level events (api_call, api_error). The transcript renders
	// each event by its type. See records.TheoryOfEventRecording.
	Event(typ string, detail string)
}

type RunOptions struct {
	// Generator is the model used for generation.
	Generator generators.Generator
	// InitialState is the starting state (without ParserState wrapping).
	// Run wraps it with ParserState internally.
	InitialState generators.State
	// StateDecorators wrap the state before the loop starts, in order.
	// Each decorator receives the state produced by the previous one.
	// The default is none; commands that need to observe state (e.g., the
	// TUI observing output content) pass their own implementations.
	// See StateDecorator.
	StateDecorators []StateDecorator
	// Components is the component set for block processing between rounds.
	// When empty, the loop runs a single round (single-shot mode).
	Components components.ComponentSet
	// BlockHandler processes blocks during streaming. May be nil.
	// If consumed is true, the block is not passed to ProcessComponents.
	BlockHandler BlockHandler
	// PhaseBuilder builds the phase chain for each round.
	PhaseBuilder func(generators.Generator) generators.Phase
	// Root is the filesystem root for ProcessComponents. Optional.
	Root *os.Root
	// HTTPClient is the HTTP client for ProcessComponents. Optional.
	HTTPClient nets.HTTPClient
	// MaxRounds limits the number of rounds. 0 means unlimited.
	MaxRounds int

	// InteractionRecorder receives generation events (contents, blocks,
	// round lifecycle) for interaction recording and self-improvement
	// analysis. When nil, the Recorder provider default is used (see the
	// InteractionRecorder provider in this package).
	// See records.TheoryOfInteractionRecording.
	InteractionRecorder InteractionRecorder

	// Command identifies the invoking command (e.g., "ai", "next"). It is
	// recorded as the session's command name when interaction recording
	// is active. When empty, "codes" is used.
	// See records.TheoryOfInteractionRecording.
	Command string

	// OnRoundStart is called before each round (including retries).
	// Used to reset per-round state (e.g., MemoryStore.Reset).
	OnRoundStart func()

	// OnRoundSuccess is called after a successful round, before
	// component processing. If it returns an error, the loop stops.
	// Used to flush per-round state (e.g., MemoryStore.Flush) and
	// collect round-level metadata (e.g., token statistics).
	// summaries contains summary block bodies extracted from the round.
	OnRoundSuccess func(state generators.State, summaries []string) error

	// OnRoundTruncated is called when a round is truncated (no summary
	// block or abnormal finish reason) and will be retried. It receives
	// the state with the truncated output, the state that will be the
	// base for the retry round, and the synthesized summary of the
	// truncated output. The callback records the truncated round in
	// round statistics. Unlike OnRoundSuccess, it must not flush
	// per-round state (e.g., MemoryStore) because the truncated round's
	// changes are discarded. See TheoryOfLoops.
	OnRoundTruncated func(truncatedState generators.State, retryBaseState generators.State, summary string) error

	// OnPhaseError is called when a phase returns an error, before
	// the loop stops. The returned state is included in the Result.
	// Used for error logging, tapping, or appending error content.
	OnPhaseError func(state generators.State, err error) generators.State

	// RetryOnMissingCompletion enables retry when no summary block is
	// found in the collected blocks after a round, or when the finish
	// reason indicates abnormal termination (e.g., "length" from
	// max-token truncation). The summary block is the mandatory
	// completion signal — no other block kind (ingest, shell, continue,
	// go-test, go-src) replaces or implies it — so every round missing
	// a summary block is retried, including rounds whose blocks trigger
	// components. See TheoryOfSummaryCompletionRetry in generate.go.
	RetryOnMissingCompletion bool
	// RetryOnError enables retry when any error occurs after the model
	// has output content during a round. The loop summarizes the
	// incomplete output (using Handoff if available),
	// appends both the error context and the summary as user content,
	// resets per-round state via OnRoundStart (which resets the
	// MemoryStore), and retries from the updated state. Errors that
	// occur before any content is output do not trigger retry. See
	// TheoryOfLoops.
	RetryOnError bool
	// MaxRetries limits retries per round when RetryOnMissingCompletion
	// or RetryOnError is true. Defaults to 3 when either is true
	// and MaxRetries is 0.
	MaxRetries int
	// Handoff summarizes incomplete output into a self-contained handoff
	// before retrying. If output is below the threshold or handoff is nil,
	// retry proceeds directly.
	Handoff func(incompleteText string) (*Handoff, error)

	// OnIdle is called when no component triggers after a round. It allows
	// the caller to provide interactive input (e.g., chat prompt) and
	// decide whether to continue with another round. If OnIdle returns
	// continue=true, a new round starts. If false or OnIdle is nil,
	// the loop ends. OnIdle is only invoked in multi-round mode (when
	// Components is non-empty). See TheoryOfIdleHandler.
	OnIdle IdleHandler
}

// formatHandoffPrompt formats the retry user prompt with the handoff content.
// See TheoryOfHandoff.
func formatHandoffPrompt(retryPrompt string, attempt, maxAttempts int) string {
	prefix := fmt.Sprintf(incompleteOutputHandoffPrefix, attempt, maxAttempts)
	return FormatHandoffPrompt(prefix, retryPrompt)
}

// Result holds the outcome of a generation loop.
type Result struct {
	// FinalState is the state after the last round (without ParserState).
	FinalState generators.State
	// RemainingBlocks are blocks not matched by any component.
	RemainingBlocks []blocks.Block
	// ParseErrors lists blocks that could not be parsed and were not
	// corrected within the maxParseErrorRounds correction budget. In
	// unattended operation, callers (e.g., the goal command) can inspect
	// this to detect silent change loss from persistently malformed
	// model output. See TheoryOfLoops.
	ParseErrors []*blocks.BlockParseError
	// Diffs are the session diffs of all changes applied through the
	// in-memory file store during this run. They are used by the review
	// loop to present the changes to a second model. See
	// TheoryOfReviewLoop in generate.go.
	Diffs []changes.FileDiff
}

// RecordState reports the given state's system prompt and contents to the
// interaction recorder and returns a state that captures future appends.
// When the recorder is nil or disabled, the state is returned unchanged
// and enabled is false. Commands that run phases outside the loop (e.g.,
// ping) use this to participate in interaction recording.
// See records.TheoryOfInteractionRecording.
func RecordState(recorder InteractionRecorder, state generators.State) (generators.State, bool) {
	if recorder == nil || !recorder.Enabled() {
		return state, false
	}
	recorder.SystemPrompt(state.SystemPrompt())
	for content := range state.Contents() {
		recorder.Content(content)
	}
	return recordedState{upstream: state, recorder: recorder}, true
}

// recordedState is a State layer that reports appended contents to an
// InteractionRecorder. It sits below ParserState so every content append
// (user input, model output, reasoning thoughts, tool calls, retry
// feedback) is captured for interaction recording. State immutability is
// preserved: AppendContent and Flush return a new recordedState.
// See records.TheoryOfInteractionRecording.
type recordedState struct {
	upstream generators.State
	recorder InteractionRecorder
}

func (s recordedState) Unwrap() generators.State {
	return s.upstream
}

func (s recordedState) Flush() (generators.State, error) {
	newUpstream, err := s.upstream.Flush()
	if err != nil {
		return nil, err
	}
	return recordedState{upstream: newUpstream, recorder: s.recorder}, nil
}

func (s recordedState) Functions() iter.Seq[*generators.Function] {
	return s.upstream.Functions()
}

func (s recordedState) SystemPrompt() string {
	return s.upstream.SystemPrompt()
}

func (s recordedState) Contents() iter.Seq[*generators.Content] {
	return s.upstream.Contents()
}

var _ generators.State = recordedState{}

func (s recordedState) AppendContent(content *generators.Content) (generators.State, error) {
	s.recorder.Content(content)
	newUpstream, err := s.upstream.AppendContent(content)
	if err != nil {
		return nil, err
	}
	return recordedState{upstream: newUpstream, recorder: s.recorder}, nil
}

func (Module) Run(
	recorder InteractionRecorder,
	logger logs.Logger,
) Run {
	return func(ctx context.Context, opts RunOptions, result *Result) iter.Seq2[Event, error] {
		if result == nil {
			result = &Result{}
		}
		return func(yield func(Event, error) bool) {
			// Determine the active interaction recorder. When the caller does
			// not pass one explicitly, the provider-injected default is used,
			// so every loop run records interactions automatically.
			// See records.TheoryOfInteractionRecording.
			rec := opts.InteractionRecorder
			if rec == nil {
				rec = recorder
			}
			opts.InteractionRecorder = rec

			// The loop state carries the mutable state of the run. Events
			// are yielded through the guarded emitEvent/emitTerminal
			// methods. See TheoryOfLoopEvents.
			ls := &loopState{
				ctx:        ctx,
				opts:       opts,
				rec:        rec,
				result:     result,
				yield:      yield,
				state:      opts.InitialState,
				maxRetries: opts.MaxRetries,
				logger:     logger,
			}
			if ls.maxRetries == 0 && (opts.RetryOnMissingCompletion || opts.RetryOnError) {
				ls.maxRetries = defaultMaxRetries
			}

			recording := rec != nil && rec.Enabled()
			if recording {
				command := opts.Command
				if command == "" {
					command = "codes"
				}
				rec.StartSession(command)
				// EndSession is deferred so every return path — including
				// errors — closes the session with the final outcome. The
				// deferred function runs when the iterator is exhausted,
				// whether the consumer pulled the terminal error and
				// resumed, or stopped the iteration early.
				defer func() {
					rec.EndSession(ls.runErr)
				}()
				// Session-level metadata is recorded as events so the
				// transcript identifies the invocation and the model that
				// powered it. See records.TheoryOfEventRecording.
				rec.Event("decision", fmt.Sprintf("command line: %s", strings.Join(os.Args, " ")))
				if opts.Generator != nil {
					spec := opts.Generator.Spec()
					rec.Event("decision", fmt.Sprintf("generator selected: name=%q model=%q family=%q effort=%q",
						spec.Name, spec.Model, spec.Family, spec.ReasoningEffort))
				}
			}

			// Report the initial system prompt and contents, then wrap the
			// state so every subsequent content append is captured for
			// interaction recording. The recordedState layer sits below
			// ParserState so both the parsed blocks and the contents
			// carrying them are recorded. Recording is skipped entirely when
			// the recorder is nil or disabled.
			// See records.TheoryOfInteractionRecording.
			ls.state, _ = RecordState(rec, ls.state)

			// Apply the state decorators after recording so decorations (e.g.,
			// observing output content for a TUI) see every subsequent
			// content append. Decorators are applied in order, each wrapping
			// the state produced by the previous one. See StateDecorator.
			for _, decorator := range opts.StateDecorators {
				if decorator != nil {
					ls.state = decorator(ls.state)
				}
			}

			// Thought summaries produced during generation join the event
			// stream: bind the ThoughtsSummarize layer's emitter, when the
			// command wrapped one, to the guarded yield. Summaries are
			// produced synchronously inside phase execution on this
			// goroutine, so the reentrant yield is safe. See
			// TheoryOfLoopEvents and TheoryOfThoughtsSummarize.
			installThoughtSummaryEmitter(ls.state, func(summary string) {
				ls.emitEvent(Event{Kind: EventThoughtSummary, Round: ls.round, Summary: summary})
			})

			// The main loop: each iteration is one generation round. A
			// round produces a summary and parts; when parts exist, the
			// next round starts. prevRoundContentCount tracks the content
			// count at the start of the current round so the round's
			// aggregated token usage can be isolated from the contents
			// appended during the round. The round's occurrences are
			// yielded as Events by runRound through the guarded yield.
			// See TheoryOfLoops and TheoryOfLoopEvents.
			prevRoundContentCount := generators.CountContents(ls.state)
			for ls.round = 1; opts.MaxRounds == 0 || ls.round <= opts.MaxRounds; ls.round++ {
				outcome, err := ls.runRound()
				if err != nil {
					// Record the failed round's token usage so token
					// consumption is traceable for every attempt, including
					// rounds that end with an error. See TheoryOfUsageLogging.
					ls.recordRoundUsage(outcome.state, prevRoundContentCount, ls.round, "error")
					ls.finishWithError(err, outcome.state)
					return
				}
				// Record the round's aggregated token usage — to the event
				// stream (the display source for a live consumer) and to the
				// logger — so token consumption is visible per round, not
				// only in the end-of-session statistics table. See
				// TheoryOfUsageLogging.
				ls.recordRoundUsage(outcome.state, prevRoundContentCount, ls.round, "")
				prevRoundContentCount = generators.CountContents(outcome.state)
				if outcome.continueNext {
					continue
				}
				if outcome.finalBlocks != nil {
					ls.finish(outcome.state, outcome.finalBlocks)
				} else {
					ls.finish(ls.state, ls.remainingBlocks)
				}
				return
			}
			ls.finish(ls.state, ls.remainingBlocks)
		}
	}
}

// ExtractIncompleteOutput collects Text and Thought parts from contents
// appended after prevCount, returning them as a single string for
// summarization. It is shared by the pipeline's retry summarization
// (handoffRetryState) and the loop's own retry paths.
func ExtractIncompleteOutput(state generators.State, prevCount int) string {
	var parts []string
	i := 0
	for c := range state.Contents() {
		if i < prevCount {
			i++
			continue
		}
		for _, p := range c.Parts {
			switch p := p.(type) {
			case generators.Text:
				parts = append(parts, string(p))
			case generators.Thought:
				parts = append(parts, string(p))
			}
		}
		i++
	}
	return strings.Join(parts, "\n")
}

// extractFinishReason scans new contents (after prevCount) for FinishReason
// parts and returns the last finish reason found. Used to detect abnormal
// termination such as max-token truncation ("length"). See
// TheoryOfSummaryCompletionRetry in generate.go.
func extractFinishReason(state generators.State, prevCount int) string {
	var reason string
	i := 0
	for c := range state.Contents() {
		if i >= prevCount {
			for _, p := range c.Parts {
				if fr, ok := p.(generators.FinishReason); ok {
					reason = string(fr)
				}
			}
		}
		i++
	}
	return reason
}

// abnormalFinishReasons lists finish reasons that indicate the output was
// truncated or ended abnormally, warranting a retry with content
// summarization. "length" (OpenAI) and "max_tokens" (some providers) mean the
// model hit the output token limit. The comparison is case-insensitive.
var abnormalFinishReasons = map[string]bool{
	"length":     true,
	"max_tokens": true,
}

// isAbnormalFinishReason reports whether the finish reason indicates
// the output was truncated or otherwise ended abnormally, warranting a
// retry with content summarization. See TheoryOfSummaryCompletionRetry in
// generate.go.
func isAbnormalFinishReason(reason string) bool {
	return abnormalFinishReasons[strings.ToLower(reason)]
}

// formatParseErrors formats collected parse errors as user content fed
// back to the model for self-correction. The message states that only the
// listed blocks were not applied and must be re-emitted, so the model does
// not re-emit already-applied blocks (which would duplicate
// ADD_BEFORE/ADD_AFTER changes). The attempt number makes the correction
// budget explicit so the model knows when it is on its final attempt and
// that persistently malformed blocks will be silently dropped. The full
// error text — block kind, delimiter, collision hints, and partial
// content — gives the model a concrete target for correction. See
// TheoryOfParseErrorCollection.
func formatParseErrors(errors []*blocks.BlockParseError, attempt, maxAttempts int) string {
	var sb strings.Builder
	sb.WriteString("[System note: The following blocks in your previous output could not be parsed and were not applied. Re-emit ONLY the corrected versions of these blocks. Do NOT re-emit any other blocks — they were applied successfully and re-emitting them would duplicate changes. ")
	fmt.Fprintf(&sb, "This is correction attempt %d of %d; if the corrected blocks remain malformed after the final attempt, they will be silently dropped. ", attempt, maxAttempts)
	sb.WriteString("After re-emitting the corrected blocks, end your response with a summary block.]\n\n")
	for _, parseErr := range errors {
		sb.WriteString(parseErr.Error())
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// decideParseErrorFeedback decides whether to feed parse errors back to
// the model for self-correction. The correction budget is cumulative per
// run: it resets only when a round produces no parse errors (returning a
// reset counter), so a model that persistently emits malformed blocks
// cannot restart the correction cycle indefinitely when other components
// keep triggering rounds. When the budget is exhausted, no feedback is
// produced and the round's parse errors are returned as uncorrected so
// the caller can record them in Result.ParseErrors. See TheoryOfLoops.
func decideParseErrorFeedback(
	roundParseErrors []*blocks.BlockParseError,
	correctionRounds int,
) (
	feedback []generators.Part,
	correctionRoundsOut int,
	skipOnRoundStart bool,
	uncorrected []*blocks.BlockParseError,
) {
	if len(roundParseErrors) == 0 {
		return nil, 0, false, nil
	}
	if correctionRounds < maxParseErrorRounds {
		correctionRounds++
		return []generators.Part{
			generators.Text(formatParseErrors(roundParseErrors, correctionRounds, maxParseErrorRounds)),
		}, correctionRounds, true, nil
	}
	return nil, correctionRounds, false, roundParseErrors
}

// appendUncorrectedParseErrors appends parse errors to the accumulated
// uncorrected list, skipping errors already recorded from previous
// rounds. A model that fails to correct tends to repeat the same
// malformed block; deduplication keeps Result.ParseErrors concise.
func appendUncorrectedParseErrors(
	accumulated []*blocks.BlockParseError,
	roundErrors []*blocks.BlockParseError,
) []*blocks.BlockParseError {
	for _, parseErr := range roundErrors {
		duplicate := false
		for _, existing := range accumulated {
			if existing.Boundary == parseErr.Boundary &&
				existing.BlockKind == parseErr.BlockKind &&
				existing.Content == parseErr.Content {
				duplicate = true
				break
			}
		}
		if !duplicate {
			accumulated = append(accumulated, parseErr)
		}
	}
	return accumulated
}
