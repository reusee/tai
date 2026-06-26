package codetypes

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

type DiffHandler interface {
	Functions() []*generators.Function
	SystemPrompt() string
	RestatePrompt() string
	Apply(root *os.Root, diffFilePath string) iter.Seq2[Hunk, error]
}