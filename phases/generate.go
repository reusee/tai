package phases

import (
	"context"
	"errors"

	"github.com/reusee/tai/generators"
)

type BuildGenerate func(generator generators.Generator, options *generators.GenerateOptions) PhaseBuilder

func (Module) BuildGenerate() BuildGenerate {
	return func(generator generators.Generator, options *generators.GenerateOptions) PhaseBuilder {
		return func(cont Phase) Phase {
			return func(ctx context.Context, state generators.State) (Phase, generators.State, error) {

				state0 := state

				for {
					newState, err := generator.Generate(ctx, state, options)
					if err != nil {
						if errors.Is(err, generators.ErrRetryable) {
							continue
						}
						return nil, nil, err
					}
					state = newState
					break
				}

				return cont, RedoCheckpoint{
					upstream:  state,
					state0:    state0,
					generator: generator,
				}, nil

			}
		}
	}
}
