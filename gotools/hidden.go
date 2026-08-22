package gotools

import (
	"slices"
	"strings"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

const TheoryOfHiddenPackages = `
go.hidden lists import-path patterns of packages that are always hidden
from the context: no code, no documentation, no go-src resolution. A
pattern ending in "/..." hides the base package and every subpackage,
matching the go tool's wildcard semantics; any other pattern hides exactly
that import path. Matching normalizes test variants to the base path
("foo [foo.test]" matches a pattern for "foo").

Hiding is enforced at the two boundaries where package content enters the
pipeline. GetFiles removes hidden packages before file discovery, so their
Go, embed, and non-Go files are never read, parsed, or token-counted, and
the go-src resolver — which searches the collected file set — reports
their symbols as not found. SimplifyFiles drops their logical packages
before categorization, so a hidden package produces no documentation block
even when loaded as a focus package, loses the context-package visibility
guarantee, and contributes no focus tokens to the dynamic context budget
(hiding an inflated focus package also shrinks the budget it would have
inflated).

Hidden wins over the automatic pipeline: focus (-pkg), context (-ctx),
and import-graph discovery cannot unhide a package. Explicit -doc
references (go.doc_patterns) are per-invocation requests for API-level
reference material and, like all flags, override config values, so they
are not subject to the hide.
`

var _ configs.Config = HiddenPatterns(nil)

// HiddenPatterns lists import-path patterns of packages that are always
// hidden from the context: no code, no documentation, no go-src symbol
// resolution. A pattern ending in "/..." hides the base package and every
// subpackage (go tool wildcard semantics); any other pattern hides exactly
// that import path. Hidden wins over focus (-pkg), context (-ctx), and
// import-graph discovery. Explicit -doc references (go.doc_patterns) are
// per-invocation requests and, like all flags, are not subject to the
// hide. Config-only: read from the "go.hidden" config path.
// See TheoryOfHiddenPackages.
type HiddenPatterns []string

// ConfigPaths returns the CUE path at which hidden package patterns are
// configured.
func (h HiddenPatterns) ConfigPaths() []string {
	return []string{"go.hidden"}
}

// HandleConfig aggregates hidden patterns from all config file roots
// additively, mirroring Envs.HandleConfig.
func (h HiddenPatterns) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := slices.Clone(h)
	for _, v := range values {
		var patterns []string
		if err := v.Decode(&patterns); err != nil {
			return nil, err
		}
		ret = append(ret, patterns...)
	}
	return &ret, nil
}

// HiddenPatterns provides the default hidden package pattern list: none.
func (Module) HiddenPatterns() HiddenPatterns {
	return nil
}

// newHiddenPackageMatcher compiles hidden patterns into a membership test
// on package import paths. It returns nil when no usable pattern is
// configured, so callers skip the check entirely on the default
// configuration. Test variants are normalized to the base path before
// matching, so hiding "foo" also hides "foo [foo.test]".
// See TheoryOfHiddenPackages.
func newHiddenPackageMatcher(patterns []string) func(pkgPath string) bool {
	if len(patterns) == 0 {
		return nil
	}
	exact := make(map[string]bool)
	var prefixes []string
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if base, ok := strings.CutSuffix(pattern, "/..."); ok && base != "" {
			// "path/..." hides the base package itself and every
			// subpackage, matching the go tool's wildcard semantics.
			// The trailing slash keeps "path/other" from matching
			// "path" plus a mere name prefix.
			exact[base] = true
			prefixes = append(prefixes, base+"/")
			continue
		}
		exact[pattern] = true
	}
	if len(exact) == 0 && len(prefixes) == 0 {
		return nil
	}
	return func(pkgPath string) bool {
		pkgPath = basePkgPath(pkgPath)
		if exact[pkgPath] {
			return true
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(pkgPath, prefix) {
				return true
			}
		}
		return false
	}
}
