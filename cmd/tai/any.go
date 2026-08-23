package main

import (
	"context"
	"os"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/modes"
)

const TheoryOfAnyCommand = `
The "any" subcommand provides code generation for arbitrary text files by
selecting the "any" PartsProvider, which delegates to anytexts.PartsProvider.
It reuses the full codes.Generate pipeline — including dynamic context,
immediate apply, shell and continue blocks, and round statistics — by wiring
codes.Module into the dscope scope. This makes "tai any" the general-purpose
entry point for non-Go code generation, complementing the Go-oriented default.
`

var AnyCommand = Command{
	Defs: []any{
		modes.ForProduction(),
		func(
			provider anytexts.PartsProvider,
		) codetypes.PartsProvider {
			return provider
		},
	},
	Main: func(
		generateWithResult codes.GenerateWithResult,
		runReview codes.RunReview,
	) {
		result, err := generateWithResult(context.Background(), os.Stdout)
		if err != nil {
			panic(err)
		}
		if err := runReview(context.Background(), os.Stdout, result.Diffs); err != nil {
			panic(err)
		}
	},
}
