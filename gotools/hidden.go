package gotools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/pathutil"
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

The working directory grants the one unhide exemption: when the process
working directory lies inside a hidden pattern's base package directory,
that pattern is not hidden, so the tool can operate on that package's
code when invoked from within it. The exemption is decided per directory,
not per module: the base import path maps to a filesystem directory only
under the module that contains the working directory (the nearest go.mod
walking up), so a hidden package elsewhere in the same module stays
hidden, and a pattern that cannot be mapped to a directory — another
module, no go.mod above the working directory, an unreadable module path —
also stays hidden.

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
// no pattern is configured. Patterns whose base package directory
// contains the process working directory are dropped first, so the prompt
// never announces a package the session can operate on. Hidden packages
// contribute no code, documentation, or go-src-resolvable symbols, so the
// section states the exclusion and its replacement behavior — work from
// the provided context and state any limitation in prose — preventing
// wasted go-src and ingest rounds on packages that can never be fetched
// through context assembly. Patterns are trimmed, sorted, and
// deduplicated so equal configurations produce byte-identical prompts,
// preserving the LLM prefix cache.
// See TheoryOfHiddenPackages.
func HiddenPackagesSystemPrompt(patterns HiddenPatterns) string {
	patterns = HiddenPatterns(unhidePatternsForWorkingDirectory(patterns))
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
	sb.WriteString("- Do NOT emit ingest blocks for files of hidden packages: their content is excluded by design and must not be read.\n")
	sb.WriteString("- Do NOT speculate about hidden packages' contents; work from the context provided. When a task seems to require a hidden package, state the limitation in prose instead of attempting to fetch it.\n")
	return sb.String()
}

// newHiddenPackageMatcher compiles hidden patterns into a membership test
// on package import paths. Patterns whose base package directory contains
// the process working directory are dropped first, so the working
// directory's own packages are never hidden. It returns nil when no
// usable pattern remains, so callers skip the check entirely on the
// default configuration. Test variants are normalized to the base path
// before matching, so hiding "foo" also hides "foo [foo.test]".
// See TheoryOfHiddenPackages.
func newHiddenPackageMatcher(patterns []string) func(pkgPath string) bool {
	patterns = unhidePatternsForWorkingDirectory(patterns)
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

// unhidePatternsForWorkingDirectory returns the patterns that remain
// hidden after dropping those whose base package directory contains the
// process working directory: when the tool is invoked from inside a
// hidden package's directory, the user is operating on that package's
// code, so the package must not be hidden. The base import path is
// mapped to a directory only under the module that contains the working
// directory; patterns that cannot be mapped stay hidden. See
// TheoryOfHiddenPackages.
func unhidePatternsForWorkingDirectory(patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return patterns
	}
	moduleRoot, modulePath := findModuleOfDir(workingDir)
	if moduleRoot == "" || modulePath == "" {
		return patterns
	}
	var kept []string
	for _, pattern := range patterns {
		base := strings.TrimSpace(pattern)
		base, _ = strings.CutSuffix(base, "/...")
		pkgDir := packageDirOfImportPath(base, modulePath, moduleRoot)
		if pkgDir != "" && dirContainsDir(pkgDir, workingDir) {
			continue
		}
		kept = append(kept, pattern)
	}
	return kept
}

// findModuleOfDir returns the directory of the nearest go.mod at or above
// dir together with the module path declared in it. Both results are
// empty when no go.mod is found or the module path cannot be read: the
// nearest go.mod marks the module boundary, so an unreadable module path
// must not fall through to an outer module. The walk itself is shared
// with every other module-root consumer through pathutil.FindGoModuleRoot.
func findModuleOfDir(dir string) (moduleRoot, modulePath string) {
	moduleRoot, ok := pathutil.FindGoModuleRoot(dir)
	if !ok {
		return "", ""
	}
	modulePath = modulePathOfGoMod(filepath.Join(moduleRoot, "go.mod"))
	if modulePath == "" {
		return "", ""
	}
	return moduleRoot, modulePath
}

// modulePathOfGoMod reads the module path declared in the go.mod file at
// path. It returns "" when the file cannot be read or declares no module
// path.
func modulePathOfGoMod(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "module ")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		// A module path holds no spaces, so the first token drops a
		// trailing comment on the directive line.
		if index := strings.IndexAny(rest, " \t"); index >= 0 {
			rest = rest[:index]
		}
		return strings.Trim(rest, `"`)
	}
	return ""
}

// packageDirOfImportPath maps an import path to a directory under
// moduleRoot by replacing the module path prefix with the module root. It
// returns "" when the import path does not belong to the module.
func packageDirOfImportPath(importPath, modulePath, moduleRoot string) string {
	switch {
	case importPath == modulePath:
		return moduleRoot
	case strings.HasPrefix(importPath, modulePath+"/"):
		return filepath.Join(moduleRoot, filepath.FromSlash(importPath[len(modulePath)+1:]))
	default:
		return ""
	}
}

// dirContainsDir reports whether target is dir itself or lies beneath it.
// The separator-joined prefix keeps a sibling whose name merely starts
// with dir's name from matching.
func dirContainsDir(dir, target string) bool {
	dir = filepath.Clean(dir)
	target = filepath.Clean(target)
	return target == dir || strings.HasPrefix(target, dir+string(filepath.Separator))
}
