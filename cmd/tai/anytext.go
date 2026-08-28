package main

import (
	"context"
	"os"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/pipeline/codetypes"
)

const TheoryOfAnyTextDefault = `
AnyTextCommand is the default command outside a Go module (see
TheoryOfCommandAutoDetection): it selects anytexts.PartsProvider for
arbitrary text files and forks SkeletonFiles(true) so the initial context
carries parsed skeletons instead of full content for every supported file
format; skeletons truncate by syntax-tree depth, and directly matched -file
targets keep full content. It runs a single generation session followed by
review of the applied changes, reusing the full generation pipeline —
dynamic context, immediate apply, shell and continue blocks, and round
statistics — wired through pipeline.Module in the dscope scope.
`

var AnyTextCommand = Command{
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
