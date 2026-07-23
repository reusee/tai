package main

import (
	"fmt"
	"os"

	"github.com/reusee/tai/codes"
)

const TheoryOfPatchCommand = `
The "patch" subcommand applies a boundary-delimited diff file (default .AI) to
the working tree without invoking any model, making it the offline counterpart
to immediate apply (see codes.TheoryOfImmediateApply) and the batch diff write
path (see codes.TheoryOfBatchDiffWrite). The generation subcommands ("go",
"any") produce and apply change blocks during generation; "patch" decouples
the apply step from generation so a pre-existing diff file can be replayed or
inspected independently. It uses the concrete codes.BoundaryDiffHandler
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
		var handler codes.BoundaryDiffHandler
		for hunk, err := range handler.Apply(root, target) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Applied %s %s\n", hunk.Op, hunk.Target)
		}
	},
}
