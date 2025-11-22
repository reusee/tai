package anytexts

import (
	"path/filepath"

	"github.com/reusee/tai/cmds"
)

var provideFileNamesFlag []string

func init() {
	cmds.Define("-file", cmds.Func(func(pattern string) {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			// ignore
			provideFileNamesFlag = append(provideFileNamesFlag, pattern)
		} else {
			provideFileNamesFlag = append(provideFileNamesFlag, paths...)
		}
	}).Desc("provide files matching the specified pattern. if any, use these files instead of current working directory"))
}

type Files []string

func (Module) Files() Files {
	return Files(provideFileNamesFlag)
}
