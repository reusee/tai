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

	// provide files from patterns (extra context)
	if len(patterns) > 0 {
		projectFiles := make(map[string]*File)
		for _, f := range files {
			projectFiles[f.Path] = f
		}

		// Collect all files from IterFiles and sort them by path to ensure
		// deterministic ordering. IterFiles may return files in file-system
		// order which varies across runs; without sorting, the order of parts
		// and the subset of files selected when the token budget is limited
		// would both be non-deterministic, causing different user prompts on
		// repeated executions.
		var extraFiles []anytexts.FileInfo
		for info, err := range c.AnyTexts().IterFiles(patterns) {
			if err != nil {
				return nil, err
			}
			extraFiles = append(extraFiles, info)
		}
		slices.SortFunc(extraFiles, func(a, b anytexts.FileInfo) int {
			return strings.Compare(a.Path, b.Path)
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
				maxTokens -= numTokens
				if *showTokenCounts {
					c.Logger().Info("extra context file", "path", info.Path, "tokens", numTokens)
				}
				totalTokens += numTokens
				parts = append(parts, generators.Text(text))

			} else {
				// binary or other media
				parts = append(parts, generators.Text("File: "+info.Path+"\n"))
				parts = append(parts, generators.FileContent{
					Content:  info.Content,
					MimeType: info.MimeType,
				})
				// approximate token count for binary content is not implemented,
				// but it will be capped by the model's overall context window.
			}
		}
	}

	files, err = c.SimplifyFiles()(files, maxTokens, countTokens)
	if err != nil {
		return nil, err
	}

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
