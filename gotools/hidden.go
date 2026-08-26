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

The hide is announced in the system prompt: pipeline.CodesComponents
appends a prompt-only component carrying HiddenPackagesSystemPrompt
(sorted, deduplicated patterns; empty when unconfigured). Visible code may
still reference a hidden package's import path, so without the notice the
model could discover the package and burn rounds on go-src fetches that
report not found. The notice also instructs the model not to read the
hidden packages' files: ingest blocks are language-neutral and mechanically
unrestricted, so file reads are governed by prompt instruction only.
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

// HiddenPackagesSystemPrompt returns a system prompt section listing the
// import-path patterns of packages hidden from this session, or "" when
// no pattern is configured. Hidden packages contribute no code,
// documentation, or go-src-resolvable symbols, so the section states the
// exclusion and its replacement behavior — work from the provided context
// and state any limitation in prose — preventing wasted go-src and read
// rounds on packages that can never be fetched through context assembly.
// Patterns are trimmed, sorted, and deduplicated so equal configurations
// produce byte-identical prompts, preserving the LLM prefix cache.
// See TheoryOfHiddenPackages.
func HiddenPackagesSystemPrompt(patterns HiddenPatterns) string {
	var cleaned []string
	for _, pattern := range patterns {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			cleaned = append(cleaned, pattern)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	slices.Sort(cleaned)
	cleaned = slices.Compact(cleaned)

	var sb strings.Builder
	sb.WriteString("**Hidden Packages:**\n\n")
	sb.WriteString("The packages matching the import-path patterns below are hidden from this session by configuration: their source, documentation, and symbols are excluded from the context, and go-src resolution reports their symbols as not found. A pattern ending in \"/...\" hides the base package and every subpackage; any other pattern hides exactly that import path.\n\n")
	for _, pattern := range cleaned {
		sb.WriteString("- ")
		sb.WriteString(pattern)
		sb.WriteString("\n")
	}
	sb.WriteString("\n**Rules:**\n")
	sb.WriteString("- Do NOT emit go-src blocks for symbols of hidden packages: they cannot be resolved, and the fetch wastes a round.\n")
	sb.WriteString("- Do NOT emit read blocks for files of hidden packages: their content is excluded by design and must not be read.\n")
	sb.WriteString("- Do NOT speculate about hidden packages' contents; work from the context provided. When a task seems to require a hidden package, state the limitation in prose instead of attempting to fetch it.\n")
	return sb.String()
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
