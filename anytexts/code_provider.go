package anytexts

import (
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"

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
	MimeType string
}

func (c CodeProvider) IterFiles(patterns []string) iter.Seq2[FileInfo, error] {
	return func(yield func(FileInfo, error) bool) {

		if len(patterns) == 0 {
			patterns = []string{"."}
		}

		var queue []string
		for _, pattern := range patterns {
			files, err := filepath.Glob(pattern)
			if err != nil {
				// use as-is
				queue = append(queue, pattern)
			} else {
				queue = append(queue, files...)
			}
		}

		handlePath := func(path string) (stop bool, err error) {
			baseName := filepath.Base(path)

			// ignore hidden files
			if baseName != "." && strings.HasPrefix(baseName, ".") {
				return false, nil
			}

			file, err := os.Open(path)
			if err != nil {
				return false, err
			}
			defer file.Close()

			stat, err := file.Stat()
			if err != nil {
				return false, err
			}

			if stat.IsDir() {
				// queue dir files
				entries, err := file.ReadDir(0)
				if err != nil {
					return false, err
				}
				for _, entry := range entries {
					queue = append(queue, filepath.Join(path, entry.Name()))
				}

			} else {
				// plain file

				// filter
				if !c.FileNameOK()(path) {
					return false, nil
				}
				if !c.NameMatch()(path) {
					return false, nil
				}

				content, err := io.ReadAll(file)
				if err != nil {
					return false, err
				}

				// mime type
				mtype := mimetype.Detect(content)
				ok := false
			l:
				for t := mtype; t != nil; t = t.Parent() {
					if t.Is("text/plain") {
						ok = true
						break
					}
					for m := range includeMimeTypes {
						if t.Is(m) {
							ok = true
							break l
						}
					}
				}

				if !ok {
					return false, nil
				}

				if !yield(FileInfo{
					Path:     path,
					Content:  content,
					MimeType: mtype.String(),
				}, nil) {
					return true, nil
				}

			}

			return false, nil
		}

		for len(queue) > 0 {
			path := queue[0]
			queue = queue[1:]
			if stop, err := handlePath(path); err != nil {
				yield(FileInfo{}, nil)
				return
			} else if stop {
				break
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

		switch {

		case strings.HasPrefix(info.MimeType, "text/plain"):
			// text

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

		default:
			// binary
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

func (c CodeProvider) Functions() []*generators.Func {
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
