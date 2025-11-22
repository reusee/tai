package main

import (
	"context"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/modes"
)

func main() {
	cmds.Execute(os.Args[1:])

	dscope.New(
		new(codes.Module),
		modes.ForProduction(),
	).Fork(
		dscope.Provide(codes.CodeProviderName("any")),
	).Call(func(
		generate codes.Generate,
	) {
		if err := generate(context.Background(), os.Stdout); err != nil {
			panic(err)
		}
	})

}
