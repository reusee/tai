package gocodes

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

type CodeProvider struct {
	GetFiles      dscope.Inject[GetFiles]
	GetFileSet    dscope.Inject[GetFileSet]
	SimplifyFiles dscope.Inject[SimplifyFiles]
	Logger        dscope.Inject[logs.Logger]
	AnyTexts      dscope.Inject[anytexts.CodeProvider]
	LoadDir       dscope.Inject[LoadDir]
}

var _ codetypes.CodeProvider = CodeProvider{}

// pendingExtraPart holds an extra file part to be added after project files.
// Deferring extra file addition ensures project files form the stable prefix
// for LLM prefix caching, while extra files (which vary by request pattern)
// form the volatile suffix.
type pendingExtraPart struct {
	part   generators.Part
	tokens int
	path   string
}

func (c CodeProvider) Functions() (ret []*generators.Function) {
	return
}

func (c CodeProvider) SystemPrompt() string {
	return ""
}

func (c CodeProvider) Parts(
	maxTokens int,
	countTokens func(string) (int, error),
	patterns []string,
) (
	parts []generators.Part,
	err error,
) {
	var totalTokens int

	files, err := c.GetFiles()()
	if err != nil {
		return nil, err
	}
	c.Logger().Info("get files done", "num files", len(files))

	// filter files based on exclusion patterns
	files = c.filterFiles(files, patterns)

	// Collect extra files from patterns for later addition after project files.
	// Extra files are placed after project files to maximize the common prefix
	// for LLM prefix caching: project files are stable across requests, while
	// extra files vary by pattern and would shift all subsequent content if
	// placed first.
	var pendingExtras []pendingExtraPart
	if len(patterns) > 0 {
		projectFiles := make(map[string]*File)
		for _, f := range files {
			projectFiles[f.Path] = f
		}

		// Collect all files from IterFiles and sort them by path first, then by modification
		// time (oldest first) as a tiebreaker. Sorting by path as the primary key ensures
		// deterministic order that resists filesystem timestamp changes, maximizing the
		// LLM prefix cache.
		var extraFiles []anytexts.FileInfo
		for info, err := range c.AnyTexts().IterFiles(patterns) {
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

			// if file is in project, mark it as do not simplify and skip adding here
			if f, ok := projectFiles[info.Path]; ok {
				f.DoNotSimplify = true
				continue
			}

			if info.IsText {
				text := "``` begin of context file " + info.Path + "\n" +
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
				// binary or other media
				pendingExtras = append(pendingExtras, pendingExtraPart{
					part: generators.Text("File: " + info.Path + "\n"),
					path: info.Path,
				})
				pendingExtras = append(pendingExtras, pendingExtraPart{
					part: generators.FileContent{
						Content:  info.Content,
						MimeType: info.MimeType,
					},
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
		if *showTokenCounts {
			c.Logger().Info("final file", "path", file.Path, "tokens", file.Confirmed.NumTokens)
		}
		totalTokens += file.Confirmed.NumTokens
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
		if *showTokenCounts && pp.tokens > 0 {
			c.Logger().Info("extra context file", "path", pp.path, "tokens", pp.tokens)
		}
		totalTokens += pp.tokens
		parts = append(parts, pp.part)
	}

	if *showTokenCounts {
		c.Logger().Info("total tokens", "tokens", totalTokens)
	}

	return
}

func (c CodeProvider) filterFiles(files []*File, patterns []string) []*File {
	if len(patterns) == 0 {
		return files
	}
	dir := string(c.LoadDir())
	var filtered []*File
	for _, file := range files {
		relPath, err := filepath.Rel(dir, file.Path)
		if err != nil {
			// If we cannot determine a relative path, include the file.
			filtered = append(filtered, file)
			continue
		}
		excluded := false
		for _, p := range patterns {
			if strings.HasPrefix(p, "!") {
				pattern := p[1:]
				if matchPattern(relPath, pattern) {
					excluded = true
					break
				}
			}
		}
		if !excluded {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func (Module) CodeProvider(
	inject dscope.InjectStruct,
) (ret CodeProvider) {
	inject(&ret)
	return
}