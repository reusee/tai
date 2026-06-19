package anytexts

import (
	"cmp"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/dscope"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

type CodeProvider struct {
	FileNameOK dscope.Inject[FileNameOK]
	NameMatch  dscope.Inject[NameMatch]
	Logger     dscope.Inject[logs.Logger]
}

var _ codetypes.CodeProvider = CodeProvider{}

type FileInfo struct {
	Path     string
	Content  []byte
	IsText   bool
	MimeType string
	ModTime  time.Time
}

func (c CodeProvider) IterFiles(patterns []string) iter.Seq2[FileInfo, error] {
	return func(yield func(FileInfo, error) bool) {

		if len(patterns) == 0 {
			patterns = []string{"."}
		}

		// Collect candidate files with modification times
		type candidate struct {
			path    string
			modTime time.Time
		}
		var candidates []candidate
		var queue []string

		for _, pattern := range patterns {
			files, err := filepath.Glob(pattern)
			if err != nil {
				// use as-is
				queue = append(queue, pattern)
			} else {
				slices.Sort(files)
				queue = append(queue, files...)
			}
		}

		for len(queue) > 0 {
			path := queue[0]
			queue = queue[1:]

			baseName := filepath.Base(path)
			// ignore hidden files
			if baseName != "." && strings.HasPrefix(baseName, ".") {
				continue
			}
			// ignore _ files
			if strings.HasPrefix(baseName, "_") {
				continue
			}

			info, err := os.Stat(path)
			if err != nil {
				yield(FileInfo{}, err)
				return
			}

			if info.IsDir() {
				entries, err := os.ReadDir(path)
				if err != nil {
					yield(FileInfo{}, err)
					return
				}
				// Sort entries by name for deterministic ordering across filesystems.
				slices.SortStableFunc(entries, func(a, b os.DirEntry) int {
					return cmp.Compare(a.Name(), b.Name())
				})
				for _, entry := range entries {
					queue = append(queue, filepath.Join(path, entry.Name()))
				}
				continue
			}

			// plain file
			if !c.FileNameOK()(path) {
				continue
			}
			if !c.NameMatch()(path) {
				continue
			}

			candidates = append(candidates, candidate{
				path:    path,
				modTime: info.ModTime(),
			})
		}

		// Sort files by modification time ascending (oldest first) to maximize
		// the common prefix across requests: older files are less likely to change
		// and therefore form a stable cacheable prefix.
		slices.SortFunc(candidates, func(a, b candidate) int {
			if a.modTime.Before(b.modTime) {
				return -1
			} else if b.modTime.Before(a.modTime) {
				return 1
			}
			return cmp.Compare(a.path, b.path)
		})

		// Process candidates in sorted order
		for _, cand := range candidates {
			content, err := os.ReadFile(cand.path)
			if err != nil {
				yield(FileInfo{}, err)
				return
			}

			// mime type
			mtype := mimetype.Detect(content)
			ok := false
			isText := false
		loop:
			for t := mtype; t != nil; t = t.Parent() {
				if t.Is("text/plain") {
					ok = true
					isText = true
					break
				}
				for m := range includeNonTextMimeTypes {
					if t.Is(m) {
						ok = true
						break loop
					}
				}
			}

			if !ok {
				continue
			}

			if !yield(FileInfo{
				Path:     cand.path,
				Content:  content,
				IsText:   isText,
				MimeType: mtype.String(),
				ModTime:  cand.modTime,
			}, nil) {
				return
			}
		}
	}
}

func (c CodeProvider) Parts(
	maxTokens int,
	countTokens func(string) (int, error),
	patterns []string,
) (
	parts []generators.Part,
	err error,
) {

	totalTokens := 0
	for info, err := range c.IterFiles(patterns) {
		if err != nil {
			return nil, err
		}

		if info.IsText {

			text := "``` begin of file " + info.Path + "\n" +
				string(info.Content) + "\n" +
				"``` end of file " + info.Path + "\n"

			numTokens, err := countTokens(text)
			if err != nil {
				return nil, err
			}
			if totalTokens+numTokens > maxTokens {
				c.Logger().Info("file skipped due to token limit",
					"at file", info.Path,
					"file tokens", numTokens,
					"total tokens", totalTokens,
					"max tokens", maxTokens,
				)
				break
			}
			totalTokens += numTokens

			parts = append(parts, generators.Text(text))

			if *debug {
				c.Logger().Info("text file",
					"path", info.Path,
					"tokens", numTokens,
					"mime type", info.MimeType,
				)
			}

		} else {
			// binary
			parts = append(parts, generators.Text("File: "+info.Path+"\n"))
			parts = append(parts, generators.FileContent{
				Content:  info.Content,
				MimeType: info.MimeType,
			})

			if *debug {
				c.Logger().Info("binary file",
					"path", info.Path,
					"mime type", info.MimeType,
				)
			}

		}

	}

	c.Logger().Info("anytexts.CodeProvider",
		"max tokens", maxTokens,
		"total tokens", totalTokens,
	)

	return
}

func (c CodeProvider) Functions() []*generators.Function {
	return nil
}

func (c CodeProvider) SystemPrompt() string {
	return ""
}

func (Module) CodeProvider(
	inject dscope.InjectStruct,
) (ret CodeProvider) {
	inject(&ret)
	return
}