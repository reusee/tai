package main

import (
	"fmt"
	"os"

	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/changes"
)

const TheoryOfPatchCommand = `
The "patch" subcommand applies a boundary-delimited diff file (default .AI) to
the working tree without invoking any model. It uses changes.ApplyDiffFile
directly, reusing the same change-block-streaming apply logic embedded in
the generation pipeline (see pipeline.TheoryOfStreamingApply) without wiring
the full generation pipeline.
`

var PatchCommand = apps.New("patch",
	"Apply a boundary-delimited diff file to the working tree",
	func(
		output Output,
		applyDiffFile changes.ApplyDiffFile,
	) apps.MainErr {
		target := ".AI"
		root, err := os.OpenRoot(".")
		if err != nil {
			return err
		}
		defer root.Close()
		for block, err := range applyDiffFile(root, target) {
			if err != nil {
				return err
			}

			fmt.Fprintf(output, "Applied %s %s\n", block.Op, block.Target)
		}

		return nil
	},
)
