package loops

import (
	"context"
	"errors"
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
2. Execute the phase chain (generate -> chat -> nil) until done
3. Unwrap ParserState to get the base state
4. Process collected blocks via ProcessComponents
5. If a component triggers (produces parts or modifies state), append the
   parts and start a new round
6. If no component triggers, the loop ends

When Components is empty, the loop runs a single round (single-shot mode),
used by commands that don't need multi-round generation or component
processing. The phase chain itself can create an interactive loop via the
chat phase (generate -> chat -> generate -> chat -> ...), so single-shot
mode still supports interactive sessions.

RetryOnMissingCompletion handles truncated output: when a round ends
without a summary block, the output was likely cut off mid-stream. The loop
summarizes the incomplete output, appends it as context, and retries from
the pre-round state. This is distinct from generator-level retry which
handles transient API errors. The retry logic and incomplete-output
summarization are unified here so all commands can opt into retry behavior
via RunOptions.

RetryOnApplyError handles model-generated errors that cause change block
application to fail (e.g., invalid target, malformed code, goimports
failure). When a BlockHandler returns an *ApplyError, the loop appends the
error message as user content so the model can correct its output, resets
per-round state via OnRoundStart (which resets the MemoryStore, discarding
all changes from the failed attempt), and retries from the updated state.
This reuses the same MemoryStore consistency guarantee as
RetryOnMissingCompletion: the disk is never left in a partially modified
state by a failed attempt, because changes are buffered in memory and only
flushed on success. ApplyError retry and missing-completion retry share the
same MaxRetries budget and retry counter, so the total number of retries per
round is bounded regardless of the error type.

The BlockHandler type uses a consumed flag: when a handler returns
consumed=true, the block is not passed to ProcessComponents. This lets
callers apply change blocks during streaming (early error detection) and
prevent double-application by the change component. When consumed=false,
the block is collected for component processing.
`

// ApplyError is returned by BlockHandler when a change block fails to apply
// due to model-generated errors (e.g., invalid target, malformed code,
// goimports failure). When RetryOnApplyError is enabled, the loop retries
// the round, resetting the MemoryStore via OnRoundStart to discard the
// failed changes. The error message is appended as user content so the
// model can correct its output in the retry. See TheoryOfLoops.
type ApplyError struct {
	Err error
}

func (e *ApplyError) Error() string {
	return e.Err.Error()
}

func (e *ApplyError) Unwrap() error {
	return e.Err
}

const applyErrorRetryPrefix = "[System note: A change block failed to apply due to an error: %s. Please correct the issue and regenerate the change blocks.]\n\n"

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

// RunOptions configures a generation loop.
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

	// RetryOnMissingCompletion enables retry when no summary or finish
	// block is found in the collected blocks after a round. This
	// handles truncated output where the model is cut off mid-stream.
	RetryOnMissingCompletion bool
	// RetryOnApplyError enables retry when a BlockHandler returns an
	// *ApplyError. The loop appends the error message as user content
	// so the model can correct its output, resets per-round state via
	// OnRoundStart (which resets the MemoryStore), and retries from
	// the updated state. See TheoryOfLoops.
	RetryOnApplyError bool
	// MaxRetries limits retries per round when RetryOnMissingCompletion
	// or RetryOnApplyError is true. Defaults to 3 when either is true
	// and MaxRetries is 0.
	MaxRetries int
	// SummarizeIncomplete summarizes incomplete output before retrying.
	// The summary is appended as user content to provide context for
	// the retry. If nil, retry proceeds without a summary.
	SummarizeIncomplete func(incompleteText string) (string, error)
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
		if maxRetries == 0 && (opts.RetryOnMissingCompletion || opts.RetryOnApplyError) {
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

			// Inner retry loop for missing completion blocks and apply errors.
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
					// Check if it's a retryable apply error. When
					// RetryOnApplyError is enabled and the error is an
					// *ApplyError, the loop appends the error message as
					// user content so the model can correct its output,
					// resets per-round state via OnRoundStart (which
					// resets the MemoryStore, discarding failed changes),
					// and retries from the updated state. See TheoryOfLoops.
					var applyErr *ApplyError
					if opts.RetryOnApplyError && errors.As(roundErr, &applyErr) && retry < maxRetries {
						// Update state to include partial content generated
						// before the error, so the model sees its prior output
						// when correcting the issue.
						state = phaseState
						var appendErr error
						state, appendErr = state.AppendContent(&generators.Content{
							Role: generators.RoleUser,
							Parts: []generators.Part{
								generators.Text(fmt.Sprintf(applyErrorRetryPrefix, applyErr.Error())),
							},
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

				// Check for completion blocks (summary).
				// Summaries were already extracted above, so check
				// roundSummaries for a summary completion signal.
				hasCompletion := len(roundSummaries) > 0

				if hasCompletion {
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
			var combinedParts []generators.Part
			var triggered bool
			var err error
			remainingBlocks, state, combinedParts, triggered, err = components.ProcessComponents(
				ctx, opts.Components, collectedBlocks, state,
				opts.Root, opts.HTTPClient, roundCounts, true,
			)
			if err != nil {
				return Result{
					FinalState:      state,
					RemainingBlocks: remainingBlocks,
				}, err
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
						}, err
					}
				}
				continue
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
