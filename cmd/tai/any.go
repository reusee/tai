package main

import (
	"context"
	"os"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/pipeline/codetypes"
)

const TheoryOfAnyCommand = `
The "any" subcommand provides code generation for arbitrary text files by
selecting the "any" PartsProvider, which delegates to anytexts.PartsProvider.
It forks SkeletonFiles(true) so the initial context carries parsed skeletons
instead of full content for every supported file format; skeletons truncate
by syntax-tree depth, and directly matched -file targets keep full content.
It reuses the full generation pipeline — including dynamic context,
immediate apply, shell and continue blocks, and round statistics — by wiring
pipeline.Module into the dscope scope. This makes "tai any" the general-purpose
entry point for non-Go code generation, complementing the Go-oriented default.
`

var AnyCommand = Command{
	Defs: []any{
		modes.ForProduction(),
		func() anytexts.SkeletonFiles {
			return true
		},
		func(
			provider anytexts.PartsProvider,
		) codetypes.PartsProvider {
			return provider
		},
	},
	Main: func(
		generateWithResult pipeline.GenerateWithResult,
		runReview pipeline.RunReview,
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
