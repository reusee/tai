package phases

import (
	"context"
	"errors"

	"github.com/reusee/tai/generators"
)

type BuildGenerate func(generator generators.Generator, cont Phase) Phase

func (Module) BuildGenerate() BuildGenerate {

	return func(generator generators.Generator, cont Phase) Phase {
		return func(ctx context.Context, state generators.State) (Phase, generators.State, error) {

			state0 := state

			for {
				newState, err := generator.Generate(ctx, state)
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
