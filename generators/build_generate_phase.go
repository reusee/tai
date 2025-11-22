package generators

import (
	"context"
	"errors"
)

type BuildGeneratePhase func(generator Generator, cont Phase) Phase

func (Module) BuildGeneratePhase() BuildGeneratePhase {

	return func(generator Generator, cont Phase) Phase {
		return func(ctx context.Context, state State) (Phase, State, error) {

			state0 := state

			for {
				newState, err := generator.Generate(ctx, state)
				if err != nil {
					if errors.Is(err, ErrRetryable) {
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
