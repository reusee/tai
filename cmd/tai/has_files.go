package main

import (
	"maps"
	"slices"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/flags"
)

const TheoryOfHasFiles = `
The tai command is a general-purpose AI tool, not exclusively a Go code
generation tool. It processes arbitrary text files and user inputs.
Therefore, change block instructions are included in the system prompt
whenever any focus file is provided, regardless of whether it is a Go file.
For non-Go files, the change block instructions already restrict operations
to file-level and text-level edits (WRITE, REPLACE, etc.), ensuring safe
and general-purpose file modification capabilities.
`

// HasFiles reports whether any focus file is provided. It is used by
// the next command's SystemPrompt and UserPrompt providers to conditionally
// include the change block system prompt (in SystemPrompt) and the change
// block restate prompt (at the end of UserPrompt), respectively.
// See TheoryOfHasFiles.
type HasFiles bool

func (Module) HasFiles(
	partsProvider anytexts.PartsProvider,
	flagFiles flags.Files,
) HasFiles {
	patterns := slices.Collect(maps.Keys(flagFiles))
	for info, err := range partsProvider.IterFiles(patterns) {
		ce(err)
		_ = info
		return true
	}
	return false
}
