package gocodes

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

type CodeProvider struct {
	GetFiles      dscope.Inject[GetFiles]
	GetFileSet    dscope.Inject[GetFileSet]
	SimplifyFiles dscope.Inject[SimplifyFiles]
	Logger        dscope.Inject[logs.Logger]
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

	// provide files from patterns
	var patternParts []generators.Part
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			content, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			patternParts = append(patternParts, generators.Text(content))
			numTokens, err := countTokens(string(content))
			if err != nil {
				return nil, err
			}
			maxTokens -= numTokens
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
		parts = append(parts, generators.Text(file.Confirmed.Content))
	}

	parts = append(parts, patternParts...)

	return
}

func (Module) CodeProvider(
	inject dscope.InjectStruct,
) (ret CodeProvider) {
	inject(&ret)
	return
}
