package codetypes

import "github.com/reusee/tai/generators"

type CodeProvider interface {
	Parts(
		maxTokens int,
		countTokens func(string) (int, error),
	) (
		parts []generators.Part,
		err error,
	)

	Functions() []*generators.Func

	SystemPrompt() string

	RootDirs() ([]string, error)
}
