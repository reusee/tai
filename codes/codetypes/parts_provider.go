package codetypes

import "github.com/reusee/tai/generators"

type PartsProvider interface {
	Parts(
		maxTokens int,
		countTokens func(string) (int, error),
		patterns []string,
	) (
		parts []generators.Part,
		err error,
	)
}
