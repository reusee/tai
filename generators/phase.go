package generators

import (
	"context"
)

type Phase func(ctx context.Context, prev State) (Phase, State, error)
