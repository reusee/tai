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
	"github.com/reusee/tai/pathutil"
	"github.com/reusee/tai/records"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	GoTools    gotools.Module
	AnyTexts   anytexts.Module
	Flags      flags.Module
	Changes    changes.Module
	Blocks     blocks.Module
	Components components.Module
	Logs       logs.Module
	Records    records.Module
	PathUtil   pathutil.Module
}
