package phases

import (
	"context"

	"github.com/reusee/tai/generators"
)

type Phase func(ctx context.Context, prev generators.State) (Phase, generators.State, error)
