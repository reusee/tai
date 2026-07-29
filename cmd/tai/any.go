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
selecting the "any" CodeProvider, which delegates to anytexts.CodeProvider.
It reuses the full codes.Generate pipeline — including dynamic context,
immediate apply, shell and continue blocks, and round statistics — by wiring
codes.Module into the dscope scope. This makes "tai any" the general-purpose
entry point for non-Go code generation, complementing the Go-oriented default.
`

var AnyCommand = Command{
	Defs: []any{
		modes.ForProduction(),
		func(
			provider anytexts.CodeProvider,
		) codetypes.CodeProvider {
			return provider
		},
	},
	Main: func(
		generate codes.Generate,
	) {
		if err := generate(context.Background(), os.Stdout); err != nil {
			panic(err)
		}
	},
}
