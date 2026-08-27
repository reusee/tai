package main

import (
	"io"
	"os"
)

const TheoryOfCommandOutput = `
Command output theory:
- Output is the dscope-resolved writer for command-level output: verdicts
  and progress messages such as ping results, applied-change notices, and
  recorded-session listings. Goal verdicts and failure notes are pipeline
  events (EventGoal) rendered in the Events tab; see TheoryOfTUI.
- The default provider writes to os.Stdout. In TUI mode runWithTUI forks
  this type to the TUI's output tab, so command output remains visible in
  the interface instead of being discarded when stdout is redirected to
  the null device.
- Generation output is captured separately by tuiOutputState and is never
  routed through this writer, so TUI mode does not duplicate model text.
  See TheoryOfTUI in cmd/tai/tui.go.
`

// Output is the command-level output writer resolved via dscope.
// Non-TUI runs write to os.Stdout; runWithTUI forks this type to the
// TUI's output tab so command verdicts and progress messages are visible
// in the interface instead of being discarded when stdout is redirected
// to the null device. Generation output is captured separately by
// tuiOutputState and is not routed through this writer. See
// TheoryOfCommandOutput and TheoryOfTUI.
type Output io.Writer

func (Module) Output() Output {
	return os.Stdout
}
