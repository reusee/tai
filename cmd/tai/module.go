package main

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/memories"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/taiconfigs"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	Configs    taiconfigs.Module
	Phases     phases.Module
	Flags      flags.Module
	Memories   memories.Module
	Modes      modes.ModuleForProduction
	Codes      codes.Module
	AnyTexts   anytexts.Module
}
