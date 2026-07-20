package main

import (
	"context"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/modes"
)

const TheoryOfGoCommand = `
The "go" subcommand provides code generation for Go files by selecting the "go"
CodeProvider, which delegates to gocodes.CodeProvider. It reuses the full
codes.Generate pipeline — including dynamic context, immediate apply, shell and
continue blocks, and round statistics — by wiring codes.Module into the dscope
scope. The -repl flag enables a REPL mode that taps the debugs infrastructure
without running generation, useful for interactive debugging. This is the
Go-oriented counterpart to the "any" subcommand for general-purpose text file
generation, and succeeds the standalone gotai command.
`

var doRepl = cmds.Switch("repl")

func init() {
	cmds.Define("go", cmds.Func(func() {
		defs = []any{
			modes.ForProduction(),
			dscope.Provide(codes.CodeProviderName("go")),
		}
		mainFunc = func(
			generate codes.Generate,
			tap debugs.Tap,
		) {
			if *doRepl {
				tap(context.Background(), "repl", map[string]any{})
				return
			}
			if err := generate(context.Background(), os.Stdout); err != nil {
				panic(err)
			}
		}
	}))
}
