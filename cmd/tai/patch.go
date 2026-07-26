package main

import (
	"fmt"
	"os"

	"github.com/reusee/tai/changes"
)

const TheoryOfPatchCommand = `
The "patch" subcommand applies a boundary-delimited diff file (default .AI) to
the working tree without invoking any model, making it the offline counterpart
to immediate apply (see codes.TheoryOfImmediateApply) and the batch diff write
path (see changes.TheoryOfBatchDiffWrite). The generation subcommands ("go",
"any") produce and apply change blocks during generation; "patch" decouples
the apply step from generation so a pre-existing diff file can be replayed or
inspected independently. It uses the concrete changes.BoundaryDiffHandler
directly, reusing the same hunk-streaming apply logic embedded in
codes.Generate without wiring the full generation pipeline.
`

var PatchCommand = Command{
	Main: func() {
		target := ".AI"
		root, err := os.OpenRoot(".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		var handler changes.BoundaryDiffHandler
		for hunk, err := range handler.Apply(root, target) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Applied %s %s\n", hunk.Op, hunk.Target)
		}
	},
}
