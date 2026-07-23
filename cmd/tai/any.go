package main

import (
	"context"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/codes"
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
		dscope.Provide(codes.CodeProviderName("any")),
	},
	Main: func(
		generate codes.Generate,
	) {
		if err := generate(context.Background(), os.Stdout); err != nil {
			panic(err)
		}
	},
}
