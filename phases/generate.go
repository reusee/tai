package phases

import (
	"context"
	"errors"
	"fmt"

	"github.com/reusee/tai/generators"
)

const TheoryOfGenerateRetry = `
The generate phase retries on ErrRetryable errors, but the retry count must be
bounded to prevent infinite output loops. Without a bound, a generator that
consistently returns ErrRetryable (e.g., Gemini's "no output" after exhausting
its internal doWithRetry, or OpenAI's persistent 429) would cause BuildGenerate
to retry indefinitely, hanging the entire generation pipeline. The bound is
set to 3 attempts, which is sufficient for transient errors while preventing
infinite loops. Generators that need finer-grained retry control (e.g., Gemini's
doWithRetry with exponential backoff) handle their own internal retries; the
BuildGenerate bound acts as an outer safety net. Additionally, doWithRetry in
gemini.go strips ErrRetryable from its return error after exhausting its own
retries, so the outer loop does not re-trigger on the same exhausted error.

When an error occurs (either non-retryable or after exhausting retries), the
phase returns the input state rather than nil. This ensures that callers like
loops.Run can pass a valid state to OnPhaseError, which may append error
information or tap debugging context. Returning nil would cause a nil pointer
dereference in OnPhaseError when it calls errState.AppendContent.
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
