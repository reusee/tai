package main

import (
	"fmt"
	"os"

	"github.com/reusee/tai/changes"
)

const TheoryOfPatchCommand = `
The "patch" subcommand applies a boundary-delimited diff file (default .AI) to
the working tree without invoking any model. It uses changes.ApplyDiffFile
directly, reusing the same change-block-streaming apply logic embedded in
the generation pipeline (see pipeline.TheoryOfStreamingApply) without wiring
the full generation pipeline.
`

var PatchCommand = Command{
	Main: func(
		output Output,
		applyDiffFile changes.ApplyDiffFile,
	) {
		target := ".AI"
		root, err := os.OpenRoot(".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for block, err := range applyDiffFile(root, target) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			// The applied notice is written to the command Output writer so
			// it is visible in the TUI's output tab. See
			// TheoryOfCommandOutput.
			fmt.Fprintf(output, "Applied %s %s\n", block.Op, block.Target)
		}
	},
}
