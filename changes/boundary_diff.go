package changes

import (
	"github.com/reusee/tai/blocks"
)

const TheoryOfBatchDiffWrite = `
The diff file is mutated in memory as change blocks are applied and persisted only once
at the end of processing (or on early exit), rather than after every change block. This
reduces I/O from O(N*S) to O(S) for N change blocks in a file of size S, without changing
the on-disk result: applied change blocks are removed and non-change blocks (e.g.,
finish summaries) are preserved exactly as before.
`

// ChangeBlockSystemPrompt returns the system prompt describing the change
// block format, composed of the shared block format prompt and the
// change-specific prompt. It is used by the change block component and the
// "next" subcommand to teach the model how to emit change blocks.
func ChangeBlockSystemPrompt() string {
	return blocks.BlockFormatSystemPrompt + "\n" + ChangeBlockPrompt
}

// ChangeBlockRestatePrompt returns the short critical reminder that reinforces
// the change block format rules. It is used by the change block component as
// its RestatePrompt field.
func ChangeBlockRestatePrompt() string {
	return ChangeBlockRestatePromptText
}
