package codes

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gocodes"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/records"
	"github.com/reusee/tai/states"
	"github.com/reusee/tai/taiconfigs"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	Configs    taiconfigs.Module
	GoCodes    gocodes.Module
	AnyTexts   anytexts.Module
	Phases     phases.Module
	Flags      flags.Module
	Changes    changes.Module
	States     states.Module
	Loops      loops.Module
	Records    records.Module
}
