package loops

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/rand/v2"
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
  simplification, and deterministic file ordering. Retry summarization
  (TheoryOfSummaryCompletionRetry in codes/generate.go) condenses truncated
  output for one-shot error recovery, not persistent history. Thought
  summarization (TheoryOfThoughtsSummarize in states/summarizer.go) writes
  to the user's screen for readability; it never feeds back as compressed
  context.

- No iterative discovery. The CodeProvider pipeline delivers all file and
  code context upfront. Request-context blocks serve external resources
  unavailable at construction time (network fetches, glob expansion), not
  as a substitute for upfront context.

- Multi-round generation is task decomposition, not conversation. Continue
  blocks split large tasks into bounded rounds; shell and go-test blocks run
  autonomous verification. The loop executes tasks; it is not a chatbot.

Features assuming a long-conversation model — dialogue-grown context, turns
summarized to free budget, conversation history as knowledge base — violate
this philosophy and are out of scope.
`

const TheoryOfLoops = `
The loops package unifies the generation loop pattern across all generation
commands (codes, ai, next). The core pattern:
1. Wrap state with ParserState to collect blocks during streaming
2. Execute the phase chain until done
3. Unwrap ParserState to get the final state and collected blocks
4. Process collected blocks through components (if any)
5. Repeat until no components trigger or MaxRounds is reached

A round is one pass through the phase chain, producing a set of blocks. A retry is
a re-execution of the phase chain within the same round, triggered by a missing
completion (no summary block) or an error after content output. Retries count as
loops in round statistics.

Retry on missing completion: a round without a summary block, or with an abnormal
finish reason (e.g., "length" from max-token truncation), was truncated mid-stream
— the generation limit hit before the model emitted its closing summary block, or
the model emitted a summary and continued until cut off. The round is retried from
the original pre-generation State. The retry process (SummarizeIncomplete)
produces both a summary of the truncated output and a continue block whose content
is the summarizer's extraction of the truncated thinking's valuable conclusions —
discoveries, decisions, facts. The summary is recorded via OnRoundTruncated, so
the truncated round appears as a separate loop in round statistics; the continue
block's content is fed to the retry round as user input, framing the retry as a
continuation consistent with the model's own continue block mechanism, letting the
retry round adopt those conclusions instead of re-deriving them — reducing the
thinking needed and lowering the chance of truncating again. See
TheoryOfIncompleteOutputSummarization in codes/generate.go.

Retry on error: an error after content output retries from the state that includes
the partial output, appending the error context and a summary of the partial output
as user content. Errors before any content output do not retry.

Retry feedback states the current attempt number (e.g., "retry attempt 1 of 3") so
the model knows how much budget remains and can prioritize correcting the error —
critical in unattended operation, where no human can intervene once the budget is
exhausted.
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

const incompleteOutputSummaryPrefix = "[System note: The previous generation was truncated before completion. This is retry attempt %d of %d. The truncated output was discarded — its structured blocks were NOT applied. Re-emit every block you intend to take effect. The continue block below carries the valuable conclusions already reached in the truncated thinking: discoveries, decisions, facts, completed work, and next steps. Adopt these conclusions and continue from where you left off; do not re-derive them, so this round needs less thinking than the truncated attempt.]\n\n"

// StateDecorator wraps a generation state before the loop starts,
// returning a new state that observes or modifies the original. The
// decorator is applied after interaction recording, so it sees every
// subsequent content append. Multiple decorators are applied in order,
// each wrapping the state produced by the previous one. See
// loops.RunOptions.StateDecorators.
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

// InteractionRecorder receives generation events for interaction recording
// and self-improvement analysis. The records package implements it with a
// sqlite-backed recorder (see records.TheoryOfInteractionRecording) that
// persists sessions and events to a single database file. When the recorder
// is disabled, Enabled returns false and the loop skips all recording work.
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
}

type RunOptions struct {
	// Generator is the model used for generation.
	Generator generators.Generator
	// InitialState is the starting state (without ParserState wrapping).
	// loops.Run wraps it with ParserState internally.
	InitialState generators.State
	// StateDecorators wrap the state before the loop starts, in order.
	// Each decorator receives the state produced by the previous one.
	// The default is none; commands that need to observe state (e.g., the
	// TUI observing FinishReason parts) pass their own implementations.
	// See loops.StateDecorator.
	StateDecorators []StateDecorator
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
	// The retry process produces both a summary of the truncated output
	// (used as the truncated round's summary in round statistics) and
	// the compressed content fed to the retry round as user input. If
	// nil, retry proceeds without a summary.
	SummarizeIncomplete func(incompleteText string) (*RetrySummary, error)

	// OnIdle is called when no component triggers after a round. It allows
	// the caller to provide interactive input (e.g., chat prompt) and
	// decide whether to continue with another round. If OnIdle returns
	// continue=true, a new round starts. If false or OnIdle is nil,
	// the loop ends. OnIdle is only invoked in multi-round mode (when
	// Components is non-empty). See phases.TheoryOfIdleHandler.
	OnIdle phases.IdleHandler
}

// RetrySummary holds the outcome of summarizing truncated output for retry.
// The retry process has two tasks: producing a summary of the truncated
// output (recorded as the truncated round's summary in round statistics)
// and producing the compressed content fed to the retry round as user
// input, framed as a continue block. See TheoryOfLoops.
type RetrySummary struct {
	// Summary is the summary of the truncated output, used as the
	// truncated round's summary in round statistics.
	Summary string
	// RetryPrompt is the compressed version of the truncated output,
	// fed to the retry round as user input.
	RetryPrompt string
}

// freshDelimiter returns a fresh trio of uncommon Chinese characters for
// use as a block delimiter in system-generated blocks. The delimiter is
// chosen randomly from a set of uncommon Chinese characters so it is
// unlikely to appear in the block body.
func freshDelimiter() string {
	const uncommonChars = "龘靐齉爩麤黿鼍爨灪虋齾齑靁齌齍齎齏爞齔齕"
	chars := []rune(uncommonChars)
	return string([]rune{
		chars[rand.IntN(len(chars))],
		chars[rand.IntN(len(chars))],
		chars[rand.IntN(len(chars))],
	})
}

// formatRetryPrompt formats the retry prompt as a continue block with a
// fresh delimiter, prefixed by the retry system note. The continue block
// frames the retry as a continuation, consistent with the model's own
// continue block mechanism. See TheoryOfLoops.
func formatRetryPrompt(retryPrompt string, attempt, maxAttempts int) string {
	delimiter := freshDelimiter()
	return fmt.Sprintf(incompleteOutputSummaryPrefix, attempt, maxAttempts) +
		"<<" + delimiter + " <continue>\n" + retryPrompt + "\n" + delimiter
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
	// TheoryOfReviewLoop in codes/generate.go.
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
) Run {
	return func(ctx context.Context, opts RunOptions) (result Result, err error) {
		// Determine the active interaction recorder. When the caller does
		// not pass one explicitly, the provider-injected default is used,
		// so every loop run records interactions automatically.
		// See records.TheoryOfInteractionRecording.
		rec := opts.InteractionRecorder
		if rec == nil {
			rec = recorder
		}
		opts.InteractionRecorder = rec
		recording := rec != nil && rec.Enabled()
		if recording {
			command := opts.Command
			if command == "" {
				command = "codes"
			}
			rec.StartSession(command)
			// EndSession is deferred so every return path — including
			// errors — closes the session with the final outcome. The
			// named result value carries the loop's error.
			defer func() {
				rec.EndSession(err)
			}()
		}

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

		// Report the initial system prompt and contents, then wrap the
		// state so every subsequent content append is captured for
		// interaction recording. The recordedState layer sits below
		// ParserState so both the parsed blocks and the contents
		// carrying them are recorded. Recording is skipped entirely when
		// the recorder is nil or disabled.
		// See records.TheoryOfInteractionRecording.
		state, _ = RecordState(rec, state)

		// Apply the state decorators after recording so decorations (e.g.,
		// observing FinishReason parts for a TUI) see every subsequent
		// content append. Decorators are applied in order, each wrapping
		// the state produced by the previous one. See loops.StateDecorator.
		for _, decorator := range opts.StateDecorators {
			if decorator != nil {
				state = decorator(state)
			}
		}

		// recordRoundError reports a failed round to the interaction
		// recorder when recording is active.
		recordRoundError := func(err error) {
			if rec != nil && rec.Enabled() {
				rec.RoundError(err)
			}
		}

		for round := 0; opts.MaxRounds == 0 || round < opts.MaxRounds; round++ {
			if rec != nil && rec.Enabled() {
				rec.RoundStart()
			}
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
					// Report every parsed block to the interaction
					// recorder, whether or not it is consumed by the
					// caller's BlockHandler.
					if rec != nil && rec.Enabled() {
						rec.Block(block)
					}
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
					// Report malformed blocks to the interaction recorder.
					if rec != nil && rec.Enabled() {
						for _, parseErr := range roundParseErrors {
							rec.ParseError(parseErr)
						}
					}
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

							// Report the failed attempt to the
							// interaction recorder.
							if rec != nil && rec.Enabled() {
								rec.RoundError(roundErr)
							}

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

							// Summarize the failed attempt's output. The
							// retry process produces both a summary
							// (recorded as the failed round's summary in
							// round statistics) and the compressed content
							// fed to the retry round as user input.
							summary := ""
							retryPrompt := ""
							if opts.SummarizeIncomplete != nil {
								incompleteText := ExtractIncompleteOutput(phaseState, prevCount)
								if incompleteText != "" {
									retrySummary, summaryErr := opts.SummarizeIncomplete(incompleteText)
									if summaryErr == nil && retrySummary != nil {
										summary = retrySummary.Summary
										retryPrompt = retrySummary.RetryPrompt
									}
								}
							}

							// Record the failed round in round statistics
							// so it appears as a separate loop.
							// See TheoryOfRoundStatistics.
							if opts.OnRoundTruncated != nil {
								if rerr := opts.OnRoundTruncated(phaseState, state, summary); rerr != nil {
									roundErr = rerr
									break
								}
							}

							// Append the continue block as the retry user
							// prompt.
							if retryPrompt != "" {
								retryParts = append(retryParts, generators.Text(
									formatRetryPrompt(retryPrompt, retry+1, maxRetries)))
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

				// Report the truncated attempt to the interaction
				// recorder.
				if rec != nil && rec.Enabled() {
					rec.RoundTruncated()
				}

				// Summarize incomplete output and retry. The retry process
				// produces both a summary of the truncated output (recorded
				// as the truncated round's summary in round statistics) and
				// a continue block whose content is the compressed version
				// of the truncated output, fed to the retry round as user
				// input. The feedback states the current attempt number so
				// the model knows how much retry budget remains.
				// See TheoryOfLoops.
				summary := ""
				retryPrompt := ""
				if opts.SummarizeIncomplete != nil {
					incompleteText := ExtractIncompleteOutput(phaseState, generators.CountContents(state))
					if incompleteText != "" {
						retrySummary, rerr := opts.SummarizeIncomplete(incompleteText)
						if rerr == nil && retrySummary != nil {
							summary = retrySummary.Summary
							retryPrompt = retrySummary.RetryPrompt
						}
					}
				}

				// Record the truncated round in round statistics so it
				// appears as a separate loop. The summary is synthesized
				// by the retry process. See TheoryOfRoundStatistics.
				if opts.OnRoundTruncated != nil {
					if rerr := opts.OnRoundTruncated(phaseState, state, summary); rerr != nil {
						roundErr = rerr
						break
					}
				}

				// Append the continue block as the retry user prompt.
				if retryPrompt != "" {
					var appendErr error
					state, appendErr = state.AppendContent(&generators.Content{
						Role: generators.RoleUser,
						Parts: []generators.Part{
							generators.Text(formatRetryPrompt(retryPrompt, retry+1, maxRetries)),
						},
					})
					if appendErr != nil {
						break
					}
				}

				// Reset for retry.
				if opts.OnRoundStart != nil {
					opts.OnRoundStart()
				}
			}

			if roundErr != nil {
				recordRoundError(roundErr)
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
				if serr := opts.OnRoundSuccess(phaseState, roundSummaries); serr != nil {
					recordRoundError(serr)
					return Result{
						FinalState:      phaseState,
						RemainingBlocks: remainingBlocks,
						ParseErrors:     uncorrectedParseErrors,
					}, serr
				}
			}

			// Report the successfully completed round to the interaction
			// recorder.
			if rec != nil && rec.Enabled() {
				rec.RoundSuccess(roundSummaries)
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
					var aerr error
					state, aerr = state.AppendContent(&generators.Content{
						Role:  generators.RoleUser,
						Parts: parseErrorParts,
					})
					if aerr != nil {
						recordRoundError(aerr)
						return Result{
							FinalState:      state,
							RemainingBlocks: remainingBlocks,
							ParseErrors:     uncorrectedParseErrors,
						}, aerr
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
			var cerr error
			roundRemaining, state, combinedParts, triggered, cerr = components.ProcessComponents(
				ctx, opts.Components, collectedBlocks, state,
				opts.Root, opts.HTTPClient, roundCounts, true,
			)
			if cerr != nil {
				recordRoundError(cerr)
				return Result{
					FinalState:      state,
					RemainingBlocks: remainingBlocks,
					ParseErrors:     uncorrectedParseErrors,
				}, cerr
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
					state, cerr = state.AppendContent(&generators.Content{
						Role:  generators.RoleUser,
						Parts: combinedParts,
					})
					if cerr != nil {
						recordRoundError(cerr)
						return Result{
							FinalState:      state,
							RemainingBlocks: remainingBlocks,
							ParseErrors:     uncorrectedParseErrors,
						}, cerr
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
				state, idleContinue, cerr = opts.OnIdle(ctx, state)
				if cerr != nil {
					recordRoundError(cerr)
					return Result{
						FinalState:      state,
						RemainingBlocks: remainingBlocks,
						ParseErrors:     uncorrectedParseErrors,
					}, cerr
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
