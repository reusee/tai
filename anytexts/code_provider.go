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
	Files      dscope.Inject[Files]
}

var _ codetypes.CodeProvider = CodeProvider{}

func (c CodeProvider) RootDirs() ([]string, error) {
	if files := c.Files(); len(files) > 0 {
		return []string(files), nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return []string{
		dir,
	}, nil
}

type FileInfo struct {
	Path     string
	Content  []byte
	MimeType string
}

func (c CodeProvider) IterFiles() iter.Seq2[FileInfo, error] {
	return func(yield func(FileInfo, error) bool) {
		queue, err := c.RootDirs()
		if err != nil {
			yield(FileInfo{}, err)
			return
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
				isText := false
				for t := mtype; t != nil; t = t.Parent() {
					if t.Is("text/plain") {
						isText = true
						break
					}
				}

				if !isText {
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
) (
	parts []generators.Part,
	err error,
) {

	totalTokens := 0
	for info, err := range c.IterFiles() {
		if err != nil {
			return nil, err
		}

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

		if *debug {
			c.Logger().Info("file",
				"path", info.Path,
				"tokens", numTokens,
				"mime type", info.MimeType,
			)
		}

		parts = append(parts, generators.Text(text))
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
