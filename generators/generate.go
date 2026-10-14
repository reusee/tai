package generators

import (
	"context"
	"errors"
	"fmt"

	"github.com/reusee/tai/logs"
)

const TheoryOfGenerateRetry = `
The generate phase retries on ErrRetryable errors, with a retry count bounded
to 3 attempts to prevent infinite output loops. Generators that need finer-
grained retry control (e.g., Gemini's Retrier.Do with exponential backoff)
handle their own internal retries; the BuildGenerate bound acts as an outer
safety net.

When an error occurs (either non-retryable or after exhausting retries), the
phase returns the input state so that callers like pipeline.Run can pass a valid
state to OnPhaseError. When the generator produced partial output before the
error, the phase returns that state instead, so the caller can detect the
content increase and trigger a retry with summarization.

Generators must preserve partial state in their return value when AppendContent
fails during streaming: the state returned with the error must include all
content appended before the failure, so the content increase is visible to
BuildGenerate and the pipeline loop can retry with feedback. OpenAI.Generate
preserves partial state because the streaming loop assigns the AppendContent
return value to ret before the error check. Gemini.Generate must do the same
by returning newState (which accumulates streamed content) instead of ret
(the pre-attempt state) from the Retrier.Do closure's error paths.
`

// BuildGenerate builds the generate phase for a generator. It lives in the
// generators package — next to Generator, State, and the retry machinery —
// because every consumer of the phase chain (the pipeline loop, records'
// analysis pass) already depends on generators.
type BuildGenerate func(generator Generator, options *GenerateOptions) PhaseBuilder

func (Module) BuildGenerate(logger logs.Logger) BuildGenerate {
	return func(generator Generator, options *GenerateOptions) PhaseBuilder {
		return func(cont Phase) Phase {
			return func(ctx context.Context, state State) (Phase, State, error) {

				state0 := state
				spec := generator.Spec()

				logger.InfoContext(ctx, "generate phase start",
					"generator", spec.Name,
					"model", spec.Model,
					"content_count", CountContents(state),
				)

				const maxRetries = 3
				var lastErr error
				for attempt := range maxRetries {
					logger.InfoContext(ctx, "generate attempt",
						"model", spec.Model,
						"attempt", attempt+1,
						"max_attempts", maxRetries,
					)
					newState, err := generator.Generate(ctx, state, options)
					if err != nil {
						lastErr = err
						logger.WarnContext(ctx, "generate attempt failed",
							"model", spec.Model,
							"attempt", attempt+1,
							"retryable", errors.Is(err, ErrRetryable),
							"error", err,
						)
						if errors.Is(err, ErrRetryable) {
							continue
						}
						// If the generator produced partial output before the
						// error, return that state so the caller (pipeline.Run)
						// can detect the content increase and trigger a retry
						// with summarization. See TheoryOfGenerateRetry.
						if newState != nil && CountContents(newState) > CountContents(state) {
							logger.WarnContext(ctx, "generate phase returns partial state",
								"model", spec.Model,
								"content_count", CountContents(newState),
								"error", err,
							)
							return nil, newState, err
						}
						// Return the input state (not nil) so callers like
						// pipeline.Run can pass a valid state to OnPhaseError.
						logger.WarnContext(ctx, "generate phase failed",
							"model", spec.Model,
							"error", err,
						)
						return nil, state, err
					}
					state = newState
					logger.InfoContext(ctx, "generate phase done",
						"model", spec.Model,
						"content_count", CountContents(state),
					)
					return cont, RedoCheckpoint{
						upstream:  state,
						State0:    state0,
						Generator: generator,
					}, nil
				}

				// All retries exhausted. Use %v (not %w) to convert lastErr
				// to a string, stripping ErrRetryable from the error chain
				// so callers do not re-trigger retries. Return the input
				// state (not nil) so callers like pipeline.Run can pass a
				// valid state to OnPhaseError.
				logger.WarnContext(ctx, "generate phase exhausted retries",
					"model", spec.Model,
					"max_attempts", maxRetries,
					"error", lastErr,
				)
				return nil, state, fmt.Errorf("generate failed after %d retries: %v", maxRetries, lastErr)

			}
		}
	}
}
