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

const inContainerEnv = "CAI_IN_CONTAINER"

func main() {
	maybeRunInContainer()

	cmds.Execute(os.Args[1:])

	dscope.New(
		new(codes.Module),
		modes.ForProduction(),
	).Fork(
		dscope.Provide(codes.CodeProviderName("go")),
		dscope.Provide(codes.DefaultDiffHandlerName("unified")),
	).Call(func(
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
	})

}

var doRepl = cmds.Switch("repl")
