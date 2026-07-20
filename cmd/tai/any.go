package main

import (
	"context"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
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

func init() {
	cmds.Define("any", cmds.Func(func() {
		defs = []any{
			modes.ForProduction(),
			dscope.Provide(codes.CodeProviderName("any")),
		}
		mainFunc = func(
			generate codes.Generate,
		) {
			if err := generate(context.Background(), os.Stdout); err != nil {
				panic(err)
			}
		}
	}))
}
