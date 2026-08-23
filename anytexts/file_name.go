package anytexts

import (
	"regexp"

	"github.com/reusee/tai/flags"
)

// TheoryOfMatchFiltering documents the cross-command contract of the
// -match flag (flags.Match).
const TheoryOfMatchFiltering = `
The -match flag (flags.Match) is a regex include filter over file paths.
NameMatch compiles the patterns once and accepts a path when any pattern
matches; an empty Match (flag unset) accepts everything, so the filter is
inert by default. Every file collection path applies the same NameMatch so
the flag behaves uniformly across commands: anytexts.IterFiles filters its
directory traversal, gotools.PartsProvider filters the collected project
files during context assembly (before SimplifyFiles, so the visibility
budget is computed from the filtered set), and the ai command filters its
globbed -file results. go-src symbol resolution reads the unfiltered file
set: -match governs context inclusion, not the symbol lookup source.
`

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
