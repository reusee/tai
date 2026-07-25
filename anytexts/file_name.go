package anytexts

import (
	"regexp"

	"github.com/reusee/tai/flags"
)

type FileNameOK func(name string) bool

func (Module) FileNameOK() FileNameOK {
	return func(name string) bool {
		return true
	}
}

type NameMatch func(string) bool

func (Module) NameMatch(
	match flags.Match,
) NameMatch {
	if len(match) == 0 {
		return func(string) bool {
			return true
		}
	}
	var patterns []*regexp.Regexp
	for pattern := range match {
		patterns = append(patterns, regexp.MustCompile(pattern))
	}
	return func(path string) bool {
		for _, pattern := range patterns {
			if pattern.MatchString(path) {
				return true
			}
		}
		return false
	}
}
