package gotools

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

const TheoryOfDocPatterns = `
The -doc flag includes the documentation of a Go package in the model context
as reference. Each package path is rendered with go doc -all -cmd and
wrapped with "begin of context package" markers so the model can identify the
documentation reference boundary. The invocation deliberately omits the -u
flag so the reference stays focused on exported symbols. This gives the model
precise API-level reference material for packages that may not be loaded as
project files — for example, an external package being integrated — or for
project packages whose full source would consume too much of the context
budget.

Doc parts are appended in PartsProvider.Parts after extra files, as part of the
volatile suffix: project files form the stable prefix for LLM prefix caching,
while doc content varies by request like extra files. Documentation is
truncated from the end when the token budget is exhausted, so packages
included in smaller-budget requests appear at the same positions in
larger-budget requests.

The rendering reuses renderPackageDoc, the same function that produces
full-doc (VisibilityDoc) package documentation for the visibility system, so
the marker format and the go doc invocation are consistent across the
codebase. A failure of go doc for a user-specified package path aborts
context assembly with an error, matching the fail-fast behavior of other
user-provided loader arguments (e.g., -pkg patterns).
`

// DocPatterns configs.Config implementation.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = DocPatterns(nil)

// DocPatterns lists Go package paths whose documentation (go doc -all
// -cmd) is included in the model context as reference. The -doc flag adds
// a package path; the "go.doc_patterns" config path provides a list of
// package paths. See TheoryOfDocPatterns.
type DocPatterns []string

func (Module) DocPatterns() DocPatterns {
	return nil
}

var _ flags.Flag = DocPatterns(nil)

func (d DocPatterns) Keys() map[string]string {
	return map[string]string{
		"-doc": "Add a package whose documentation (go doc -all -cmd) is included in the context",
	}
}

func (d DocPatterns) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expected package path, got empty")
	}
	ret := append(slices.Clone(d), args[0])
	return &ret, args[1:], nil
}

func (d DocPatterns) ConfigPaths() []string {
	return []string{"go.doc_patterns"}
}

func (d DocPatterns) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := slices.Clone(d)
	for _, v := range values {
		var patterns []string
		if err := v.Decode(&patterns); err != nil {
			return nil, err
		}
		ret = append(ret, patterns...)
	}
	return &ret, nil
}
