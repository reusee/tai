package gocodes

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	Configs    configs.Module
}
