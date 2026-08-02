package loops

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/phases"
)

const TheoryOfLoops = `
The loops package unifies the generation loop pattern across all generation
commands (codes, ai, next). The core pattern is:
1. Wrap state with ParserState to collect blocks during streaming
2. Execute the phase chain (generate only for commands that use OnIdle,
   or generate -> chat -> nil for commands that chain chat as a phase)
   until done
3. Unwrap ParserState to get the base state
4. Process collected blocks via ProcessComponents
5. If a component triggers (produces parts or modifies state), append the
   parts and start a new round
6. If no component triggers, invoke OnIdle (if set) for interactive input.
   When OnIdle returns continue=true, a new round starts; when false, the
   loop ends. OnIdle is nil for non-interactive commands (codes, ping, next).

When Components is empty, the loop runs a single round (single-shot mode),
used by commands that don't need multi-round generation or component
processing. The phase chain itself can create an interactive loop via the
chat phase (generate -> chat -> generate -> chat -> ...), so single-shot
mode still supports interactive sessions.

RetryOnMissingCompletion handles truncated output: when a round ends
without a summary block, or when the finish reason indicates abnormal
termination (e.g., "length" from max-token truncation), the output was
likely cut off mid-stream. The loop summarizes the incomplete output,
appends it as context, and retries from the pre-round state. This is
distinct from generator-level retry which handles transient API errors.
The retry logic and incomplete-output summarization are unified here so
all commands can opt into retry behavior via RunOptions.

RetryOnError handles any error that occurs after the model has output
content during a round. When a phase returns an error and the model has
already generated content, the loop summarizes the incomplete output
(using SummarizeIncomplete if available), appends both the error context
and the summary as user content so the model can correct its output,
resets per-round state via OnRoundStart (which resets the MemoryStore,
discarding all changes from the failed attempt), and retries from the
updated state. Errors that occur before any content is output do not
trigger retry, since there is no incomplete content to summarize and the
error likely indicates a configuration or infrastructure problem rather
than a model output issue. This avoids enumerating specific error types:
any error is retryable as long as the model produced partial output.
RetryOnError and RetryOnMissingCompletion share the same MaxRetries
budget and retry counter, so the total number of retries per round is
bounded regardless of the trigger type.

The BlockHandler type uses a consumed flag: when a handler returns
consumed=true, the block is not passed to ProcessComponents. This lets
callers apply change blocks during streaming (early error detection) and
prevent double-application by the change component. When consumed=false,
the block is collected for component processing.

The OnIdle mechanism ensures automated actions are processed before
interactive user input. When a generation round ends and no component
triggers a new round, the loop invokes the OnIdle handler (if set) to
prompt the user for input. This ensures the model can chain multiple
rounds of automated execution (continue, shell, go-test) without user
intervention; the user is only prompted when no automated action is
pending. Commands without interactive input (codes, ping, next) set
OnIdle to nil, so the loop ends after the last automated action. See
phases.TheoryOfIdleHandler.

Unmatched blocks are accumulated across rounds and returned in
Result.RemainingBlocks, so callers can observe blocks emitted in any round
(e.g., the goal command's done block).
`

// ApplyError is returned by BlockHandler when a change block fails to apply
// due to model-generated errors (e.g., invalid target, malformed code,
// goimports failure). Callers may use errors.As to distinguish apply errors
// from other error types for logging or diagnostics. The loop's retry
// behavior is governed by RetryOnError, which retries any error that occurs
// after the model has output content, regardless of the specific error type.
// See TheoryOfLoops.
type ApplyError struct {
	Err error
}

func (e *ApplyError) Error() string {
	return e.Err.Error()
}

func (e *ApplyError) Unwrap() error {
	return e.Err
}

const errorRetryPrefix = "[System note: An error occurred: %s. Please correct the issue and continue.]\n\n"

const defaultMaxRetries = 3

const incompleteOutputSummaryPrefix = "[System note: The previous generation was truncated before completion. Below is a summary of the incomplete output. Please continue from where you left off, incorporating the context below.]\n\n"

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

		for round := 0; opts.MaxRounds == 0 || round < opts.MaxRounds; round++ {
			if opts.OnRoundStart != nil {
				opts.OnRoundStart()
			}

			var collectedBlocks []blocks.Block
			var roundSummaries []string
			phaseState := state
			var roundErr error

			// Inner retry loop for missing completion and errors with content output.
			for retry := 0; ; retry++ {
				collectedBlocks = nil

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
					// content is output do not trigger retry.
					// See TheoryOfLoops.
					if opts.RetryOnError && retry < maxRetries {
						prevCount := countContents(state)
						if countContents(phaseState) > prevCount {
							state = phaseState

							var retryParts []generators.Part
							retryParts = append(retryParts, generators.Text(
								fmt.Sprintf(errorRetryPrefix, roundErr.Error())))

							if opts.SummarizeIncomplete != nil {
								incompleteText := extractIncompleteOutput(phaseState, prevCount)
								if incompleteText != "" {
									if summaryText, summaryErr := opts.SummarizeIncomplete(incompleteText); summaryErr == nil && summaryText != "" {
										retryParts = append(retryParts, generators.Text(
											incompleteOutputSummaryPrefix+summaryText))
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
				finishReason := extractFinishReason(phaseState, countContents(state))
				isAbnormalFinish := isAbnormalFinishReason(finishReason)

				if hasCompletion && !isAbnormalFinish {
					break
				}
				if retry >= maxRetries {
					break
				}

				// Summarize incomplete output and retry.
				if opts.SummarizeIncomplete != nil {
					incompleteText := extractIncompleteOutput(phaseState, countContents(state))
					if incompleteText != "" {
						summaryText, err := opts.SummarizeIncomplete(incompleteText)
						if err == nil && summaryText != "" {
							var appendErr error
							state, appendErr = state.AppendContent(&generators.Content{
								Role: generators.RoleUser,
								Parts: []generators.Part{
									generators.Text(incompleteOutputSummaryPrefix + summaryText),
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
				}, roundErr
			}

			// OnRoundSuccess hook.
			if opts.OnRoundSuccess != nil {
				if err := opts.OnRoundSuccess(phaseState, roundSummaries); err != nil {
					return Result{
						FinalState:      phaseState,
						RemainingBlocks: remainingBlocks,
					}, err
				}
			}

			state = phaseState

			// Single-shot mode: no component processing.
			if len(opts.Components) == 0 {
				return Result{
					FinalState:      state,
					RemainingBlocks: collectedBlocks,
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
				}, err
			}
			remainingBlocks = append(remainingBlocks, roundRemaining...)

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
		}, nil
	}
}

// countContents returns the number of contents in the state.
func countContents(state generators.State) int {
	count := 0
	for range state.Contents() {
		count++
	}
	return count
}

// extractIncompleteOutput collects Text and Thought parts from contents
// appended after prevCount, returning them as a single string for
// summarization.
func extractIncompleteOutput(state generators.State, prevCount int) string {
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
