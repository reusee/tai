package phases

import (
	"context"
	"errors"
	"fmt"

	"github.com/reusee/tai/generators"
)

const TheoryOfGenerateRetry = `
The generate phase retries on ErrRetryable errors, with a retry count bounded
to 3 attempts to prevent infinite output loops. Generators that need finer-
grained retry control (e.g., Gemini's doWithRetry with exponential backoff)
handle their own internal retries; the BuildGenerate bound acts as an outer
safety net. doWithRetry in gemini.go strips ErrRetryable from its return error
after exhausting its own retries, so the outer loop does not re-trigger on the
same exhausted error.

When an error occurs (either non-retryable or after exhausting retries), the
phase returns the input state so that callers like loops.Run can pass a valid
state to OnPhaseError.
`

type BuildGenerate func(generator generators.Generator, options *generators.GenerateOptions) PhaseBuilder

func (Module) BuildGenerate() BuildGenerate {
	return func(generator generators.Generator, options *generators.GenerateOptions) PhaseBuilder {
		return func(cont Phase) Phase {
			return func(ctx context.Context, state generators.State) (Phase, generators.State, error) {

				state0 := state

				const maxRetries = 3
				var lastErr error
				for range maxRetries {
					newState, err := generator.Generate(ctx, state, options)
					if err != nil {
						lastErr = err
						if errors.Is(err, generators.ErrRetryable) {
							continue
						}
						// If the generator produced partial output before the
						// error, return that state so the caller (loops.Run)
						// can detect the content increase and trigger a retry
						// with summarization. See TheoryOfGenerateRetry.
						if newState != nil && generators.CountContents(newState) > generators.CountContents(state) {
							return nil, newState, err
						}
						// Return the input state (not nil) so callers like
						// loops.Run can pass a valid state to OnPhaseError.
						return nil, state, err
					}
					state = newState
					return cont, RedoCheckpoint{
						upstream:  state,
						state0:    state0,
						generator: generator,
					}, nil
				}

				// All retries exhausted. Use %v (not %w) to convert lastErr
				// to a string, stripping ErrRetryable from the error chain
				// so callers do not re-trigger retries. Return the input
				// state (not nil) so callers like loops.Run can pass a
				// valid state to OnPhaseError.
				return nil, state, fmt.Errorf("generate failed after %d retries: %v", maxRetries, lastErr)

			}
		}
	}
}
