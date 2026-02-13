package taitape

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/logs"
)

type Module struct {
	dscope.Module
}

func (Module) VM(
	logger logs.Logger,
) func(path string) *VM {
	return func(path string) *VM {
		return &VM{
			FilePath: path,
			Logger:   logger,
		}
	}
}

