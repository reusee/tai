package gocodes

import (
	"fmt"

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
}

var _ codetypes.CodeProvider = CodeProvider{}

func (c CodeProvider) Functions() (ret []*generators.Func) {
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

	// provide files from patterns (extra context)
	if len(patterns) > 0 {
		for info, err := range c.AnyTexts().IterFiles(patterns) {
			if err != nil {
				return nil, err
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

	files, err := c.GetFiles()()
	if err != nil {
		return nil, err
	}
	c.Logger().Info("get files done", "num files", len(files))
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

func (Module) CodeProvider(
	inject dscope.InjectStruct,
) (ret CodeProvider) {
	inject(&ret)
	return
}
