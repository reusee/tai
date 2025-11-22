package main

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/taiconfigs"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	Configs    taiconfigs.Module
}
