package main

import (
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/flags"
)

const TheoryOfCommandAutoDetection = `
When no subcommand is given, the default command is auto-detected:
Module.InGoModule walks up the directory tree from the working directory
looking for a go.mod file. Inside a Go module the default is
GoModuleCommand — the Go parts provider (gotools.PartsProvider) running
goal mode; outside one it is AnyTextCommand — the anytexts.PartsProvider
(with skeletons) running a single generation session with review.
`

// Command provides the default Runner when no subcommand is given,
// auto-detected from the environment: GoModuleCommand inside a Go module,
// AnyTextCommand outside one. The selectable subcommands register their
// own keys through apps.App. See TheoryOfCommandAutoDetection and
// apps.TheoryOfApps.
func (Module) Command(
	inGoModule InGoModule,
) (ret apps.Runner) {
	if inGoModule {
		return GoModuleCommand
	}
	return AnyTextCommand
}

var (
	_ apps.Runner = GoModuleCommand
	_ flags.Flag  = AICommand
)
