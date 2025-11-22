package debugs

import (
	"context"
	"maps"
	"slices"

	"github.com/reusee/tai/logs"
	"go.starlark.net/repl"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type Tap func(ctx context.Context, what string, globals map[string]any)

func (Module) Tap(
	logger logs.Logger,
) Tap {
	return func(ctx context.Context, what string, globals map[string]any) {
		logger.InfoContext(ctx, "tap: "+what,
			"globals", slices.Collect(maps.Keys(globals)),
		)
		defer func() {
			logger.InfoContext(ctx, "tap end: "+what)
		}()

		mappings := make(starlark.StringDict)
		for name, value := range globals {
			mappings[name] = toStarlarkValue(value)
		}

		thread := &starlark.Thread{
			Name: "repl",
		}
		repl.REPLOptions(&syntax.FileOptions{
			Set:             true,
			While:           true,
			TopLevelControl: true,
		}, thread, mappings)
	}
}
