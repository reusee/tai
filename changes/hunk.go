package changes

import (
	"iter"
	"os"

	"github.com/reusee/tai/generators"
)

// Hunk represents a single modification unit parsed from AI output.
type Hunk struct {
	Op       string
	Target   string
	FilePath string
	Body     string
	Raw      string
}

// DiffHandler is the interface for handlers that translate model-emitted
// change blocks into byte-level edits on source files. A handler contributes
// system/restate prompts describing the block format and an Apply method that
// streams parsed Hunks from a diff file while applying each one to the working
// tree rooted at root.
type DiffHandler interface {
	Functions() []*generators.Function
	SystemPrompt() string
	RestatePrompt() string
	Apply(root *os.Root, diffFilePath string) iter.Seq2[Hunk, error]
}