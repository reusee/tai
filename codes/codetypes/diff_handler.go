package codetypes

import "github.com/reusee/tai/generators"

type DiffHandler interface {
	Functions() []*generators.Function
	SystemPrompt() string
	RestatePrompt() string
}
