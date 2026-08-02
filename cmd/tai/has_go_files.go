package main

import (
	"maps"
	"slices"
	"strings"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/flags"
)

// HasGoFiles reports whether any focus file is a Go file. It is used by
// the next command's SystemPrompt and UserPrompt providers to conditionally
// include the change block system prompt (in SystemPrompt) and the change
// block restate prompt (at the end of UserPrompt), respectively.
type HasGoFiles bool

func (Module) HasGoFiles(
	codeProvider anytexts.CodeProvider,
	flagFiles flags.Files,
) HasGoFiles {
	patterns := slices.Collect(maps.Keys(flagFiles))
	for info, err := range codeProvider.IterFiles(patterns) {
		ce(err)
		if strings.HasSuffix(info.Path, ".go") {
			return true
		}
	}
	return false
}
