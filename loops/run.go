package loops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/phases"
)

// TheoryOfContextPhilosophy articulates the system's single-shot context
// construction philosophy. All context the model needs is assembled upfront
// through pruning, simplification, and token budgeting — not discovered
// through multi-turn conversation. This constant is referenced by other
// theories to prevent suggestions that rely on long-conversation patterns.
const TheoryOfContextPhilosophy = `
The system provides all context the model needs in a single generation
request, rather than discovering it through multi-turn conversation. This
single-shot approach sets the system apart from mainstream agentic agents
that grow context through dialogue.

Upfront context construction: file contents, dependency graphs, system
prompts, and task instructions are assembled before the first generation
call. Pruning removes irrelevant files. Simplification strips function bodies
and comments from non-focus packages. Token budgeting caps total input size.
The model then reasons over the complete picture and produces changes ready
for human review.

Architectural constraints:

- No long conversations. The system does not accumulate dialogue across
  tasks. Each invocation builds fresh context from the filesystem state.
  The ai command's interactive mode lets the user type messages across
  turns, but each turn sends the full accumulated context to the model,
  not a compressed fragment.

- No conversation compression. The system never summarizes old dialogue to
  free token budget. Context is managed solely through pruning, AST-level
  simplification, and deterministic file ordering. Retry summarization
  (TheoryOfSummaryCompletionRetry in codes/generate.go) condenses truncated
  output for one-shot error recovery; it does not persist as compressed
  history. Thought summarization (TheoryOfThoughtsSummarize in
  states/summarizer.go) writes to the user's screen for readability; it
  never feeds back into the model as compressed context.

- No iterative discovery. The CodeProvider pipeline delivers all file and
  code context upfront. The request-context block exists for external
  resources unavailable at construction time (network fetches, glob
  expansion), not as a substitute for upfront context.

- Multi-round generation is task decomposition, not conversation. Continue
  blocks split large tasks into bounded rounds. Shell and go-test blocks
  run autonomous verification. The generation loop executes tasks; it is
  not a chatbot.

Features that assume a long-conversation model — growing context through
dialogue, summarizing old turns to free budget, treating conversation
history as a knowledge base — violate this philosophy and are out of scope.
`

const TheoryOfLoops = `
The loops package unifies the generation loop pattern across all generation
commands (codes, ai, next). The core pattern is:
1. Wrap state with ParserState to collect blocks during streaming
2. Execute the phase chain until done
3. Unwrap ParserState to get the base state
4. Process collected blocks via ProcessComponents
5. If a component triggers (produces parts or modifies state), append the
   parts and start a new round
6. If no component triggers, invoke OnIdle (if set) for interactive input.
   When OnIdle returns continue=true, a new round starts; when false, the
   loop ends. OnIdle is nil for non-interactive commands (codes, ping, next).

When Components is empty, the loop runs a single round (single-shot mode).

Parse errors — blocks whose closing delimiter is missing or malformed — are
collected from ParserState after each round and fed back as user content in the
next round, so the model can self-correct its malformed blocks. The correction
budget is bounded by maxParseErrorRounds: feedback stops after that many
consecutive rounds with parse errors, and the uncorrected errors are surfaced
via Result.ParseErrors. The budget resets when a round produces no parse
errors. Feedback states the attempt number (e.g., "correction attempt 1 of 3")
so the model knows when it is on its final attempt.

RetryOnMissingCompletion handles truncated output: when a round ends without
a summary block, or when the finish reason indicates abnormal termination (e.g.,
"length" from max-token truncation), the output was likely cut off mid-stream.
The loop summarizes the incomplete output, appends it as context, and retries
from the pre-round state.

RetryOnError handles any error that occurs after the model has output content
during a round. The loop summarizes the incomplete output (using
SummarizeIncomplete if available), appends both the error context and the
summary as user content, resets per-round state via OnRoundStart (which resets
the MemoryStore, discarding all changes from the failed attempt), and retries
from the updated state. Errors that occur before any content is output do not
trigger retry. For change block apply errors (changes.ApplyError), the
feedback adds specific guidance: because the retry discards all change blocks
from the failed attempt, the model is instructed to re-emit every intended
change block, correcting the one that failed.

Because the retry discards the failed attempt's output entirely — structured
blocks (change, shell, go-test, continue) in it were not applied — both retry
feedback messages instruct the model to re-emit every block it intends to take
effect. Without this instruction, the model may interpret "continue from where
you left off" as emitting only the continuation text, silently losing blocks
that were generated but not applied.

Retry feedback states the current attempt number (e.g., "retry attempt 1 of 3")
so the model knows how much retry budget remains and can prioritize correcting
the error. This is especially important in unattended operation, where no human
can intervene when the budget is exhausted.
`

const errorRetryPrefix = "[System note: An error occurred: %s. This is retry attempt %d of %d. The failed attempt's output was discarded — its structured blocks were NOT applied. Re-emit every block you intend to take effect, then correct the issue and continue.]\n\n"

const defaultMaxRetries = 3

// maxParseErrorRounds bounds the number of rounds that feed parse errors
// back to the model for self-correction. The bound is cumulative per run:
// it resets only when a round produces no parse errors, so a model that
// persistently emits malformed blocks cannot restart the correction cycle
// indefinitely when other components keep triggering rounds. When the
// bound is reached, feedback stops and the uncorrected parse errors are
// recorded in Result.ParseErrors. See TheoryOfLoops.
const maxParseErrorRounds = 3

const incompleteOutputSummaryPrefix = "[System note: The previous generation was truncated before completion. This is retry attempt %d of %d. The truncated output was discarded — its structured blocks were NOT applied. Re-emit every block you intend to take effect. Below is a summary of the incomplete output; continue from where you left off, incorporating the context below.]\n\n"

// Run executes generation rounds in a loop. Each round wraps the state
// with ParserState, executes the phase chain, processes blocks via
// components, and continues if a component triggers a new round.
// When Components is empty, the loop runs a single round (single-shot
// mode). See TheoryOfLoops.
type Run func(ctx context.Context, opts RunOptions) (Result, error)

// BlockHandler processes a block during streaming. If consumed is true,
// the block is not passed to ProcessComponents. If err is non-nil,
// streaming stops immediately. See TheoryOfLoops.
type BlockHandler func(block blocks.Block) (consumed bool, err error)

// RunOptions configures a generation loop run.
type RunOptions struct {
	// Generator is the model used for generation.
	Generator generators.Generator
	// InitialState is the starting state (without ParserState wrapping).
	// loops.Run wraps it with ParserState internally.
	InitialState generators.State
	// Components is the component set for block processing between rounds.
	// When empty, the loop runs a single round (single-shot mode).
	Components components.ComponentSet
	// BlockHandler processes blocks during streaming. May be nil.
	// If consumed is true, the block is not passed to ProcessComponents.
	BlockHandler BlockHandler
	// PhaseBuilder builds the phase chain for each round.
	PhaseBuilder func(generators.Generator) phases.Phase
	// Root is the filesystem root for ProcessComponents. Optional.
	Root *os.Root
	// HTTPClient is the HTTP client for ProcessComponents. Optional.
	HTTPClient nets.HTTPClient
	// MaxRounds limits the number of rounds. 0 means unlimited.
	MaxRounds int

	// OnRoundStart is called before each round (including retries).
	// Used to reset per-round state (e.g., MemoryStore.Reset).
	OnRoundStart func()

	// OnRoundSuccess is called after a successful round, before
	// component processing. If it returns an error, the loop stops.
	// Used to flush per-round state (e.g., MemoryStore.Flush) and
	// collect round-level metadata (e.g., token statistics).
	// summaries contains summary block bodies extracted from the round.
	OnRoundSuccess func(state generators.State, summaries []string) error

	// OnPhaseError is called when a phase returns an error, before
	// the loop stops. The returned state is included in the Result.
	// Used for error logging, tapping, or appending error content.
	OnPhaseError func(state generators.State, err error) generators.State

	// RetryOnMissingCompletion enables retry when no summary block is
	// found in the collected blocks after a round, or when the finish
	// reason indicates abnormal termination (e.g., "length" from
	// max-token truncation). This handles truncated output where the
	// model is cut off mid-stream.
	RetryOnMissingCompletion bool
	// RetryOnError enables retry when any error occurs after the model
	// has output content during a round. The loop summarizes the
	// incomplete output (using SummarizeIncomplete if available),
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
	// SummarizeIncomplete summarizes incomplete output before retrying.
	// The summary is appended as user content to provide context for
	// the retry. If nil, retry proceeds without a summary.
	SummarizeIncomplete func(incompleteText string) (string, error)

	// OnIdle is called when no component triggers after a round. It allows
	// the caller to provide interactive input (e.g., chat prompt) and
	// decide whether to continue with another round. If OnIdle returns
	// continue=true, a new round starts. If false or OnIdle is nil,
	// the loop ends. OnIdle is only invoked in multi-round mode (when
	// Components is non-empty). See phases.TheoryOfIdleHandler.
	OnIdle phases.IdleHandler
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
}

func (Module) Run() Run {
	return func(ctx context.Context, opts RunOptions) (Result, error) {
		state := opts.InitialState
		var remainingBlocks []blocks.Block
		roundCounts := make(map[string]int)
		maxRetries := opts.MaxRetries
		if maxRetries == 0 && (opts.RetryOnMissingCompletion || opts.RetryOnError) {
			maxRetries = defaultMaxRetries
		}

		// parseErrorCorrectionRounds counts rounds that produced parse
		// errors and received correction feedback since the last clean
		// round. The correction budget is cumulative per run: it resets
		// only when a round produces no parse errors, so a model that
		// persistently emits malformed blocks cannot restart the
		// correction cycle indefinitely when other components keep
		// triggering rounds. When the budget is exhausted, feedback stops
		// and the uncorrected parse errors are recorded in the Result.
		// See TheoryOfLoops.
		parseErrorCorrectionRounds := 0
		// uncorrectedParseErrors accumulates parse errors from rounds
		// where the correction budget was exhausted. They are surfaced in
		// Result.ParseErrors so unattended callers can detect silent
		// change loss. See TheoryOfLoops.
		var uncorrectedParseErrors []*blocks.BlockParseError
		// skipOnRoundStart is set when a round produced parse errors and
		// its changes were already flushed by OnRoundSuccess; it prevents
		// the next round's OnRoundStart from resetting per-round state
		// (e.g., MemoryStore) that would discard the successfully applied
		// changes before the model corrects the malformed blocks.
		// See TheoryOfParseErrorCollection.
		skipOnRoundStart := false

		for round := 0; opts.MaxRounds == 0 || round < opts.MaxRounds; round++ {
			if opts.OnRoundStart != nil && !skipOnRoundStart {
				opts.OnRoundStart()
			}

			var collectedBlocks []blocks.Block
			var roundSummaries []string
			var roundParseErrors []*blocks.BlockParseError
			phaseState := state
			var roundErr error

			// Inner retry loop for missing completion and errors with content output.
			for retry := 0; ; retry++ {
				collectedBlocks = nil
				roundParseErrors = nil

				// Create parser handler that collects blocks and
				// optionally invokes the caller's BlockHandler.
				parserHandler := func(block blocks.Block) error {
					if opts.BlockHandler != nil {
						consumed, err := opts.BlockHandler(block)
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
				parserState := blocks.NewParserState(state, parserHandler)
				wrappedState := generators.State(parserState)

				// Build and execute phase chain.
				phase := opts.PhaseBuilder(opts.Generator)
				for phase != nil {
					var err error
					phase, wrappedState, err = phase(ctx, wrappedState)
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
					phaseState = state
				} else if ps, ok := generators.As[*blocks.ParserState](wrappedState); ok {
					phaseState = ps.Unwrap()
					// Collect parse errors from the stream so they can be
					// fed back to the model for self-correction.
					// See TheoryOfParseErrorCollection.
					roundParseErrors = ps.ParseErrors()
				} else {
					phaseState = wrappedState
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
					// feedback states the current attempt number so the
					// model knows how much retry budget remains.
					// See TheoryOfLoops.
					if opts.RetryOnError && retry < maxRetries {
						prevCount := generators.CountContents(state)
						if generators.CountContents(phaseState) > prevCount {
							state = phaseState

							var retryParts []generators.Part
							retryParts = append(retryParts, generators.Text(
								fmt.Sprintf(errorRetryPrefix, roundErr.Error(), retry+1, maxRetries)))

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

							if opts.SummarizeIncomplete != nil {
								incompleteText := ExtractIncompleteOutput(phaseState, prevCount)
								if incompleteText != "" {
									if summaryText, summaryErr := opts.SummarizeIncomplete(incompleteText); summaryErr == nil && summaryText != "" {
										retryParts = append(retryParts, generators.Text(
											fmt.Sprintf(incompleteOutputSummaryPrefix, retry+1, maxRetries)+summaryText))
									}
								}
							}

							var appendErr error
							state, appendErr = state.AppendContent(&generators.Content{
								Role:  generators.RoleUser,
								Parts: retryParts,
							})
							if appendErr != nil {
								break
							}
							roundErr = nil
							// Reset for retry: OnRoundStart resets the
							// MemoryStore, discarding all changes from the
							// failed attempt. This preserves filesystem
							// consistency — the disk is never left in a
							// partially modified state by a failed attempt.
							if opts.OnRoundStart != nil {
								opts.OnRoundStart()
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
				if !opts.RetryOnMissingCompletion {
					break
				}

				// Check for completion: a summary block signals normal
				// completion, but an abnormal finish reason (e.g.,
				// "length" from max-token truncation) overrides the
				// summary signal and triggers retry. See
				// TheoryOfSummaryCompletionRetry in codes/generate.go.
				hasCompletion := len(roundSummaries) > 0
				finishReason := extractFinishReason(phaseState, generators.CountContents(state))
				isAbnormalFinish := isAbnormalFinishReason(finishReason)

				if hasCompletion && !isAbnormalFinish {
					break
				}
				if retry >= maxRetries {
					break
				}

				// Summarize incomplete output and retry. The feedback
				// states the current attempt number so the model knows
				// how much retry budget remains. See TheoryOfLoops.
				if opts.SummarizeIncomplete != nil {
					incompleteText := ExtractIncompleteOutput(phaseState, generators.CountContents(state))
					if incompleteText != "" {
						summaryText, err := opts.SummarizeIncomplete(incompleteText)
						if err == nil && summaryText != "" {
							var appendErr error
							state, appendErr = state.AppendContent(&generators.Content{
								Role: generators.RoleUser,
								Parts: []generators.Part{
									generators.Text(fmt.Sprintf(incompleteOutputSummaryPrefix, retry+1, maxRetries) + summaryText),
								},
							})
							if appendErr != nil {
								break
							}
						}
					}
				}

				// Reset for retry.
				if opts.OnRoundStart != nil {
					opts.OnRoundStart()
				}
			}

			if roundErr != nil {
				if opts.OnPhaseError != nil {
					phaseState = opts.OnPhaseError(phaseState, roundErr)
				}
				return Result{
					FinalState:      phaseState,
					RemainingBlocks: remainingBlocks,
					ParseErrors:     uncorrectedParseErrors,
				}, roundErr
			}

			// OnRoundSuccess hook.
			if opts.OnRoundSuccess != nil {
				if err := opts.OnRoundSuccess(phaseState, roundSummaries); err != nil {
					return Result{
						FinalState:      phaseState,
						RemainingBlocks: remainingBlocks,
						ParseErrors:     uncorrectedParseErrors,
					}, err
				}
			}

			state = phaseState

			// Parse error handling: feed parse errors back to the model
			// for self-correction in the next round. A round that
			// produced parse errors always triggers another round (within
			// the maxParseErrorRounds correction budget), so the model can
			// re-emit the malformed blocks in corrected form. The changes
			// from blocks that parsed successfully were already flushed
			// by OnRoundSuccess; skipOnRoundStart prevents the correction
			// round from resetting per-round state that would discard
			// them. When the budget is exhausted, feedback stops and the
			// uncorrected parse errors are recorded in the Result.
			// See TheoryOfParseErrorCollection.
			var parseErrorParts []generators.Part
			var roundUncorrected []*blocks.BlockParseError
			parseErrorParts, parseErrorCorrectionRounds, skipOnRoundStart, roundUncorrected =
				decideParseErrorFeedback(roundParseErrors, parseErrorCorrectionRounds)
			if len(roundUncorrected) > 0 {
				uncorrectedParseErrors = appendUncorrectedParseErrors(uncorrectedParseErrors, roundUncorrected)
			}

			// Single-shot mode: no component processing. When parse errors
			// were collected, feed them back and continue with a
			// correction round instead of ending the loop.
			if len(opts.Components) == 0 {
				if len(parseErrorParts) > 0 {
					var err error
					state, err = state.AppendContent(&generators.Content{
						Role:  generators.RoleUser,
						Parts: parseErrorParts,
					})
					if err != nil {
						return Result{
							FinalState:      state,
							RemainingBlocks: remainingBlocks,
							ParseErrors:     uncorrectedParseErrors,
						}, err
					}
					continue
				}
				return Result{
					FinalState:      state,
					RemainingBlocks: collectedBlocks,
					ParseErrors:     uncorrectedParseErrors,
				}, nil
			}

			// Process components.
			// Unmatched blocks are accumulated across rounds so that
			// blocks not consumed by any component (e.g., a goal done
			// block emitted in a round that also triggers another round)
			// remain available in Result.RemainingBlocks. See
			// TheoryOfGoalCommand in cmd/tai/goal.go.
			var roundRemaining []blocks.Block
			var combinedParts []generators.Part
			var triggered bool
			var err error
			roundRemaining, state, combinedParts, triggered, err = components.ProcessComponents(
				ctx, opts.Components, collectedBlocks, state,
				opts.Root, opts.HTTPClient, roundCounts, true,
			)
			if err != nil {
				return Result{
					FinalState:      state,
					RemainingBlocks: remainingBlocks,
					ParseErrors:     uncorrectedParseErrors,
				}, err
			}
			remainingBlocks = append(remainingBlocks, roundRemaining...)

			// Prepend parse error feedback to component output parts so
			// the model corrects malformed blocks alongside processing
			// component results. Parse errors always trigger a new round.
			if len(parseErrorParts) > 0 {
				combinedParts = append(parseErrorParts, combinedParts...)
				triggered = true
			}

			if triggered {
				if len(combinedParts) > 0 {
					state, err = state.AppendContent(&generators.Content{
						Role:  generators.RoleUser,
						Parts: combinedParts,
					})
					if err != nil {
						return Result{
							FinalState:      state,
							RemainingBlocks: remainingBlocks,
							ParseErrors:     uncorrectedParseErrors,
						}, err
					}
				}
				continue
			}

			// No component triggered. Try OnIdle (e.g., chat prompt) to
			// allow interactive user input before ending the loop.
			// Automated actions (continue, shell, go-test,
			// request-context) are processed first via component
			// processing above; OnIdle is only invoked when no
			// automated action is pending.
			// See phases.TheoryOfIdleHandler.
			if opts.OnIdle != nil {
				var idleContinue bool
				state, idleContinue, err = opts.OnIdle(ctx, state)
				if err != nil {
					return Result{
						FinalState:      state,
						RemainingBlocks: remainingBlocks,
						ParseErrors:     uncorrectedParseErrors,
					}, err
				}
				if idleContinue {
					continue
				}
			}

			break
		}

		return Result{
			FinalState:      state,
			RemainingBlocks: remainingBlocks,
			ParseErrors:     uncorrectedParseErrors,
		}, nil
	}
}

// ExtractIncompleteOutput collects Text and Thought parts from contents
// appended after prevCount, returning them as a single string for
// summarization. It is shared by the codes module's retry summarization
// (codes.summarizeRetryState) and the loop's own retry paths.
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
// TheoryOfSummaryCompletionRetry in codes/generate.go.
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
// summarization. "length" (OpenAI) and "max_tokens" (some providers) mean
// the model hit the output token limit. The comparison is case-insensitive.
var abnormalFinishReasons = map[string]bool{
	"length":     true,
	"max_tokens": true,
}

// isAbnormalFinishReason reports whether the finish reason indicates the
// output was truncated or otherwise ended abnormally, warranting a retry
// with content summarization. See TheoryOfSummaryCompletionRetry in
// codes/generate.go.
func isAbnormalFinishReason(reason string) bool {
	return abnormalFinishReasons[strings.ToLower(reason)]
}

// formatParseErrors formats collected parse errors as user content fed
// back to the model for self-correction. The message states that only
// the listed blocks were not applied and must be re-emitted, so the
// model does not re-emit already-applied blocks (which would duplicate
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
