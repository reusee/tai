package main

import (
	"fmt"

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

// Command is a tai subcommand: Defs forks the scope for the command's
// providers and Main runs the command. It is also a flags.Flag, so
// flags.Parse selects the subcommand by its name.
type Command struct {
	// Interactive reports whether the command supports multi-turn
	// conversation: interactive commands call pipeline.ChatInput while
	// running — the ai command's idle handler and the next command's
	// chat phase. runWithTUI copies it into the TUI, which renders the
	// chat input bar only in interactive sessions; every other command
	// (goal mode, any, ping, patch, record) hides the bar and keeps the
	// Output pane's full height. See TheoryOfTUIChatInput.
	Interactive bool
	Defs        []any
	Main        any
}

// Command provides the default command when no subcommand is given,
// auto-detected from the environment: GoModuleCommand inside a Go module,
// AnyTextCommand outside one. See TheoryOfCommandAutoDetection.
func (Module) Command(
	inGoModule InGoModule,
) (ret Command) {
	if inGoModule {
		return GoModuleCommand
	}
	return AnyTextCommand
}

var _ flags.Flag = Command{}

func (c Command) Keys() map[string]string {
	return map[string]string{
		"next":   "Identify and execute the most valuable next step",
		"ai":     "Start an interactive AI chat session with memory",
		"patch":  "Apply a boundary-delimited diff file to the working tree",
		"ping":   "Test whether a model is reachable and can emit blocks in the required format",
		"record": "Record interaction sessions and analyze them for self-improvement",
	}
}

func (c Command) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	switch key {

	case "next":
		ret := NextCommand
		return &ret, args, nil

	case "ai":
		ret := AICommand
		return &ret, args, nil

	case "patch":
		ret := PatchCommand
		return &ret, args, nil

	case "ping":
		ret := PingCommand
		return &ret, args, nil

	case "record":
		ret := RecordCommand
		return &ret, args, nil

	}

	panic(fmt.Errorf("command not handle: %s", key))
}
