package codetypes

import "github.com/reusee/tai/generators"

type DiffHandler interface {
	Functions() []*generators.Func
	SystemPrompt() string
	RestatePrompt() string
}
