package gotools

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/pathutil"
	"github.com/reusee/tai/pipeline/codetypes"
)

type PartsProvider struct {
	GetFiles        dscope.Inject[GetFiles]
	NameMatch       dscope.Inject[anytexts.NameMatch]
	SimplifyFiles   dscope.Inject[SimplifyFiles]
	Logger          dscope.Inject[logs.Logger]
	AnyTexts        dscope.Inject[anytexts.PartsProvider]
	LoadDir         dscope.Inject[LoadDir]
	ShowTokenCounts dscope.Inject[ShowTokenCounts]
	Envs            dscope.Inject[Envs]
	Workspace       dscope.Inject[Workspace]
	DocPatterns     dscope.Inject[DocPatterns]
}

var _ codetypes.PartsProvider = PartsProvider{}

const TheoryOfExtraFileContext = `
Extra files requested via patterns are appended after project files to preserve
the LLM prefix cache (project files form the stable prefix, extra files form the
volatile suffix). Binary extra files are wrapped with begin/end markers matching
the text file format, including the MIME type, so the model can identify the
attachment boundary.
`

// pendingExtraPart holds an extra file part to be added after project files.
// Deferring extra file addition ensures project files form the stable prefix
// for LLM prefix caching, while extra files (which vary by request pattern)
// form the volatile suffix.
type pendingExtraPart struct {
	part   generators.Part
	tokens int
	path   string
}

func (c PartsProvider) Parts(
	maxTokens int,
	countTokens func(string) (int, error),
	patterns []string,
) (
	parts []generators.Part,
	err error,
) {
	var totalTokens int
	// Accumulate the token composition of the final prompt as parts are
	// added: focus project files, context project files, extra files from
	// -file patterns, and package documentation from -doc patterns. The
	// composition is logged at the end so the user can see where the
	// context tokens are spent. See TheoryOfTokenComposition.
	var focusTokens, contextTokens, extraTokens, docTokens int

	files, err := c.GetFiles()()
	if err != nil {
		return nil, err
	}
	c.Logger().Info("get files done", "num files", len(files))

	// filter files based on exclusion patterns
	files = c.filterFiles(files, patterns)

	// Check whether focus files (root package files) are within writable
	// directories. Files outside writable directories are marked as
	// read-only rather than rejected, because the model can still use
	// their content as reference even though it cannot modify them.
	// The read-only marker in the file context instructs the model not
	// to emit change blocks targeting these files.
	// See TheoryOfFocusFileDirectoryCheck in anytexts/parts_provider.go.
	for _, file := range files {
		if !file.PackageIsRoot {
			continue
		}
		outside, err := pathutil.IsOutsideWritableDirs(file.Path)
		if err != nil {
			return nil, fmt.Errorf("check focus file path: %w", err)
		}
		if outside {
			file.ReadOnly = true
		}
	}

	// Separate inclusion and exclusion patterns. Exclusion patterns use a
	// "!" prefix; they are not file paths and must not be passed to IterFiles,
	// which would attempt to os.Lstat them and abort iteration on error.
	// See TheoryOfExclusionPatterns.
	var includePatterns, excludePatterns []string
	for _, p := range patterns {
		if strings.HasPrefix(p, "!") {
			excludePatterns = append(excludePatterns, p[1:])
		} else {
			includePatterns = append(includePatterns, p)
		}
	}

	// Filter out large embed files (>64KB) unless explicitly requested via
	// inclusion patterns. See TheoryOfEmbedFileSizeLimit.
	const maxEmbedFileSize = 64 << 10
	{
		var filteredFiles []*File
		for _, f := range files {
			if f.IsEmbed && len(f.Content) > maxEmbedFileSize {
				relPath, err := filepath.Rel(string(c.LoadDir()), f.Path)
				if err != nil {
					filteredFiles = append(filteredFiles, f)
					continue
				}
				explicitlyRequested := false
				for _, pattern := range includePatterns {
					if matchPattern(relPath, pattern) {
						explicitlyRequested = true
						break
					}
				}
				if !explicitlyRequested {
					continue
				}
			}
			filteredFiles = append(filteredFiles, f)
		}
		files = filteredFiles
	}

	// Collect extra files from patterns for later addition after project files.
	// Extra files are placed after project files to maximize the common prefix
	// for LLM prefix caching: project files are stable across requests, while
	// extra files vary by pattern and would shift all subsequent content if
	// placed first.
	var pendingExtras []pendingExtraPart
	if len(includePatterns) > 0 {
		projectFiles := make(map[string]*File)
		for _, f := range files {
			projectFiles[f.Path] = f
		}

		// Collect all files from IterFiles and sort them by path first, then by modification
		// time (oldest first) as a tiebreaker. Sorting by path as the primary key ensures
		// deterministic order that resists filesystem timestamp changes, maximizing the
		// LLM prefix cache.
		var extraFiles []anytexts.FileInfo
		for info, err := range c.AnyTexts().IterFiles(includePatterns) {
			if err != nil {
				return nil, err
			}
			extraFiles = append(extraFiles, info)
		}
		slices.SortStableFunc(extraFiles, func(a, b anytexts.FileInfo) int {
			if a.Path != b.Path {
				return strings.Compare(a.Path, b.Path)
			}
			if a.ModTime.Before(b.ModTime) {
				return -1
			} else if b.ModTime.Before(a.ModTime) {
				return 1
			}
			return 0
		})

		// Deduplicate extra files by path to guard against IterFiles returning
		// the same file multiple times (e.g., when patterns overlap). Without
		// deduplication, duplicate additions would inflate the token budget and
		// could shift which project files survive simplification, leading to
		// non-deterministic prompts.
		seenExtraPaths := make(map[string]bool)
		for _, info := range extraFiles {
			if seenExtraPaths[info.Path] {
				continue
			}
			seenExtraPaths[info.Path] = true

			// Skip files excluded by patterns.
			if isExcludedPath(info.Path, excludePatterns) {
				continue
			}

			// if file is in project, mark it as do not simplify and skip adding here
			if f, ok := projectFiles[info.Path]; ok {
				f.DoNotSimplify = true
				continue
			}

			if info.IsText {

				readOnlyNote := ""
				if info.ReadOnly {
					readOnlyNote = " (read-only)"
				}
				text := "``` begin of context file " + info.Path + readOnlyNote + "\n" +
					string(info.Content) + "\n" +
					"``` end of context file " + info.Path + "\n"

				numTokens, err := countTokens(text)
				if err != nil {
					return nil, err
				}
				if numTokens > maxTokens {
					continue
				}
				pendingExtras = append(pendingExtras, pendingExtraPart{
					part:   generators.Text(text),
					tokens: numTokens,
					path:   info.Path,
				})

			} else {
				// Binary extra files are wrapped with begin/end markers matching
				// the text file format. See TheoryOfExtraFileContext.
				readOnlyNote := ""
				if info.ReadOnly {
					readOnlyNote = ", read-only"
				}
				beginMarker := "``` begin of context file " + info.Path + " (binary, " + info.MimeType + ")" + readOnlyNote + "\n"
				endMarker := "\n``` end of context file " + info.Path + "\n"

				// Count text markers for the token budget. Binary content itself
				// cannot be accurately counted by a text tokenizer, but the markers
				// are text and must be accounted for to prevent budget overflow.
				markerTokens, err := countTokens(beginMarker + endMarker)
				if err != nil {
					return nil, err
				}

				pendingExtras = append(pendingExtras, pendingExtraPart{
					part:   generators.Text(beginMarker),
					tokens: markerTokens,
					path:   info.Path,
				})
				pendingExtras = append(pendingExtras, pendingExtraPart{
					part: generators.FileContent{
						Content:  info.Content,
						MimeType: info.MimeType,
					},
					path: info.Path,
				})
				pendingExtras = append(pendingExtras, pendingExtraPart{
					part: generators.Text(endMarker),
					path: info.Path,
				})
			}
		}
	}

	// Simplify project files with the full token budget.
	// Using the full budget ensures project file simplification is deterministic
	// regardless of extra file sizes, preserving the LLM prefix cache.
	files, err = c.SimplifyFiles()(files, maxTokens, countTokens)
	if err != nil {
		return nil, err
	}

	// Add project files first — these form the stable prefix for LLM caching.
	for _, file := range files {
		if len(file.Confirmed.Content) == 0 {
			panic(fmt.Errorf("empty file: %+v", file))
		}
		if c.ShowTokenCounts() {
			c.Logger().Info("final file", "path", file.Path, "tokens", file.Confirmed.NumTokens)
		}
		totalTokens += file.Confirmed.NumTokens
		if file.PackageIsRoot {
			focusTokens += file.Confirmed.NumTokens
		} else {
			contextTokens += file.Confirmed.NumTokens
		}
		parts = append(parts, generators.Text(file.Confirmed.Content))
	}

	// Add extra files after project files — these form the volatile suffix.
	// Extra files vary by request pattern; placing them last ensures they
	// cannot shift the position of stable project file content.
	//
	// Token budget truncation uses break (not continue) to preserve prefix
	// cache stability: when maxTokens varies across requests (e.g., switching
	// models with different context windows), truncating from the end ensures
	// that files included in smaller-budget requests remain at the exact same
	// positions in larger-budget requests. With continue, a large file in the
	// middle would be skipped but subsequent smaller files would still be
	// appended, shifting their positions and invalidating the cache for all
	// content from that point onward.
	for _, pp := range pendingExtras {
		if pp.tokens > 0 && totalTokens+pp.tokens > maxTokens && maxTokens > 0 {
			break
		}
		if c.ShowTokenCounts() && pp.tokens > 0 {
			c.Logger().Info("extra context file", "path", pp.path, "tokens", pp.tokens)
		}
		totalTokens += pp.tokens
		extraTokens += pp.tokens
		parts = append(parts, pp.part)
	}

	// Add package documentation for -doc patterns after extra files so
	// project files remain the stable prefix for LLM prefix caching; doc
	// content varies by request and belongs to the volatile suffix. Like
	// extra files, documentation is truncated from the end when the token
	// budget is exhausted, so packages included in smaller-budget requests
	// appear at the same positions in larger-budget requests. The rendering
	// reuses renderPackageDoc (also used for level-1 package visibility),
	// keeping the marker format and the go doc invocation consistent across
	// the codebase. A failed go doc for a user-specified package aborts
	// context assembly, matching the fail-fast behavior of other
	// user-provided loader arguments. See TheoryOfDocPatterns.
	if docPatterns := c.DocPatterns(); len(docPatterns) > 0 {
		dir := string(c.LoadDir())
		if workspace := c.Workspace(); workspace != "" {
			dir = string(workspace)
		}
		envs := c.Envs()
		for _, pkgPath := range docPatterns {
			// Stop once the budget is exhausted rather than running the
			// remaining go doc subprocesses whose output would not be added.
			if maxTokens > 0 && totalTokens >= maxTokens {
				break
			}
			content, tokens, err := renderPackageDoc(pkgPath, dir, []string(envs), countTokens)
			if err != nil {
				return nil, fmt.Errorf("go doc %s: %w", pkgPath, err)
			}
			if maxTokens > 0 && totalTokens+tokens > maxTokens {
				break
			}
			totalTokens += tokens
			docTokens += tokens
			parts = append(parts, generators.Text(content))
			if c.ShowTokenCounts() {
				c.Logger().Info("package doc", "path", pkgPath, "tokens", tokens)
			}
		}
	}

	// Log the assembled token composition: focus project files, context
	// project files, extra files, and package documentation. Together
	// with the allocation composition logged by SimplifyFiles, this shows
	// where every token in the final prompt comes from. Per-file logs
	// remain gated by -show-token-counts; this summary is always shown.
	// See TheoryOfTokenComposition.
	c.Logger().Info("token composition",
		"focus", focusTokens,
		"context", contextTokens,
		"extra", extraTokens,
		"doc", docTokens,
		"total", totalTokens,
	)

	// The working directory hint is appended after all file contents so
	// the model can construct correct absolute paths for change block
	// file-path attributes. The path is dynamic — it changes per
	// invocation — so it is placed at the end, keeping the file contents
	// byte-identical in the LLM prefix cache across runs in different
	// directories. See anytexts.TheoryOfWorkingDirectoryHint.
	if part := anytexts.WorkingDirectoryPart(); part != nil {
		parts = append(parts, part)
	}

	return
}

const TheoryOfExclusionPatterns = `
Exclusion patterns (prefixed with "!") filter files from the context provided
to the model. A non-glob pattern like "pkg" matches both a file named "pkg"
and all files under the "pkg/" directory, acting as a directory prefix filter.
Glob patterns (containing *, ?, or []) are matched via matchPattern, which
supports ** for recursive directory matching. Patterns are also matched against
the path with leading ".." components stripped, so a file in a sibling
workspace module (relPath "../mod2/README.md") is excluded by the pattern
"mod2/README.md". Slash-less patterns (e.g., "*.md" or "README.md") additionally
match the path's basename at any depth, following gitignore-style semantics:
automatically-included markdown files are excluded by name or extension
regardless of their directory. Exclusion patterns must be separated from
inclusion patterns before being passed to IterFiles, because IterFiles treats
all patterns as file paths to glob-expand.
`

const TheoryOfEmbedFileSizeLimit = `
Embed files larger than 64KB are excluded from project file context by default
to prevent large embedded assets (e.g., binary blobs, templates, static files)
from consuming the token budget. These files can still be included explicitly
via the -file flag, which adds them as extra context files. The 64KB threshold
is chosen because embed files below this size are typically configuration or
small templates that provide useful context, while larger files are almost
always static assets that add noise without aiding code generation.
`

// isExcludedPath checks whether the given relative path is excluded by any
// exclusion pattern. Supports glob matching via matchPattern, plus directory
// prefix matching for non-glob patterns (e.g., "pkg" excludes all files
// under the "pkg" directory). The path is also matched with leading ".."
// components stripped, so patterns are evaluated against the project or
// workspace root rather than the load directory's position. Slash-less
// patterns (e.g., "*.md" or "README.md") additionally match the path's
// basename at any depth, so automatically-included markdown files are
// excluded by name or extension regardless of their directory.
// See TheoryOfExclusionPatterns.
func isExcludedPath(relPath string, excludePatterns []string) bool {
	cleanedRelPath := filepath.Clean(relPath)
	baseName := filepath.Base(cleanedRelPath)

	// Strip leading ".." components so patterns are matched against the
	// path relative to the project or workspace root. A file in a sibling
	// workspace module has relPath "../mod2/README.md"; the pattern
	// "mod2/README.md" must still match it. See TheoryOfExclusionPatterns.
	trimmedRelPath := cleanedRelPath
	for strings.HasPrefix(trimmedRelPath, ".."+string(filepath.Separator)) {
		trimmedRelPath = strings.TrimPrefix(trimmedRelPath, ".."+string(filepath.Separator))
	}

	for _, pattern := range excludePatterns {
		cleanedPattern := filepath.Clean(pattern)

		// Glob match against the full path and the path with leading
		// ".." stripped.
		if matchPattern(relPath, pattern) || matchPattern(trimmedRelPath, pattern) {
			return true
		}

		// Directory-prefix match for non-glob patterns: "pkg" excludes
		// both "pkg" itself and everything under "pkg/".
		if !strings.ContainsAny(cleanedPattern, "*?[") &&
			(cleanedRelPath == cleanedPattern ||
				strings.HasPrefix(cleanedRelPath, cleanedPattern+string(filepath.Separator)) ||
				trimmedRelPath == cleanedPattern ||
				strings.HasPrefix(trimmedRelPath, cleanedPattern+string(filepath.Separator))) {
			return true
		}

		// Slash-less patterns match the basename at any depth, so
		// automatically-included markdown files in subdirectories or
		// sibling workspace modules are excluded by name or extension.
		if !strings.Contains(cleanedPattern, string(filepath.Separator)) &&
			matchPattern(baseName, cleanedPattern) {
			return true
		}
	}
	return false
}

// filterFiles applies the include and exclude filters to the collected
// project files: the -match regex include filter (NameMatch) always runs,
// and "!"-prefixed exclusion patterns run when any are present. Both
// filters build new slices in order — files is the cached GetFiles
// result, so its backing array must not be mutated — and preserve the
// deterministic input order. See anytexts.TheoryOfMatchFiltering.
func (c PartsProvider) filterFiles(files []*File, patterns []string) []*File {
	nameMatch := c.NameMatch()
	matched := make([]*File, 0, len(files))
	for _, f := range files {
		if nameMatch(f.Path) {
			matched = append(matched, f)
		}
	}
	files = matched

	if len(patterns) == 0 {
		return files
	}
	dir := string(c.LoadDir())
	var excludePatterns []string
	for _, p := range patterns {
		if strings.HasPrefix(p, "!") {
			excludePatterns = append(excludePatterns, p[1:])
		}
	}
	if len(excludePatterns) == 0 {
		return files
	}
	var filtered []*File
	for _, file := range files {
		relPath, err := filepath.Rel(dir, file.Path)
		if err != nil {
			// If we cannot determine a relative path, include the file.
			filtered = append(filtered, file)
			continue
		}
		if !isExcludedPath(relPath, excludePatterns) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func (Module) PartsProvider(
	inject dscope.InjectStruct,
) (ret PartsProvider) {
	inject(&ret)
	return
}
