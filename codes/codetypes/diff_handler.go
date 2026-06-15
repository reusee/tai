package codetypes

import (
	"os"

	"github.com/reusee/tai/generators"
)

type DiffHandler interface {
	Functions() []*generators.Function
	SystemPrompt() string
	RestatePrompt() string
	Apply(root *os.Root, diffFilePath string) error
}
