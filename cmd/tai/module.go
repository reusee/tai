package main

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/memories"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/records"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	Flags      flags.Module
	Memories   memories.Module
	Modes      modes.ModuleForProduction
	Pipeline   pipeline.Module
	AnyTexts   anytexts.Module
	Records    records.Module
}
