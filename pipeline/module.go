package pipeline

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gotools"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/records"
)

// Module unifies the former codes, loops, and states modules into one
// pipeline module: the generation loop, the generation pipeline built on
// it, and the state layers (handoff, thought summarization) resolve in a
// single dscope scope.
type Module struct {
	dscope.Module
	Generators generators.Module
	GoTools    gotools.Module
	AnyTexts   anytexts.Module
	Phases     phases.Module
	Flags      flags.Module
	Changes    changes.Module
	Blocks     blocks.Module
	Components components.Module
	Logs       logs.Module
	Records    records.Module
}
