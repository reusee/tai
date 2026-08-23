package pipeline

import (
	"io"
)

// ThoughtSummaryWriter is the writer that receives periodic thought
// summaries when thought summarization is enabled (-summarize-thoughts).
// The default provider returns nil, in which case summaries are written to
// the generation output writer — the same stream the raw thoughts would
// have used. A display front-end (e.g., tai's TUI) forks this type to
// route the summaries to its own display, so the user sees the condensed
// reasoning instead of the raw thoughts. The summaries are written as
// plain text, never as blocks, so a destination that scans for blocks
// treats them as prose.
// See TheoryOfThoughtsSummarize.
type ThoughtSummaryWriter io.Writer

func (Module) ThoughtSummaryWriter() ThoughtSummaryWriter {
	return nil
}
