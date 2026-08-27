package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/gotools"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/pipeline/codetypes"
)

const TheoryOfGoCommand = `
The "go" subcommand provides code generation for Go files by selecting the "go"
PartsProvider, which delegates to gotools.PartsProvider. It wires pipeline.Module
into the dscope scope and always runs goal mode (pipeline.GoalRun): repeated
fresh generation loops until a done block is confirmed or a loop applies
no change blocks, with each loop's
outcome carried into the next loop's system prompt as pipeline.GoalFeedback,
alongside the summaries of all previous loops (pipeline.GoalLoopSummaries).
The system prompt is forked through pipeline.GoalSystemPromptText, which
composes the base codes prompt, the goal system prompt, and the component
sections, appending the previous-loop summaries and the loop feedback at the
end. Generation and review
stream to os.Stdout; banners, verdicts, and aggregated statistics go to the
command Output writer. The -repl flag enables a REPL mode that taps the
debugs infrastructure without running generation, useful for interactive
debugging. This is the Go-oriented counterpart to the "any" subcommand for
general-purpose text file generation.

When no subcommand is provided and the current directory is inside a Go module
(a go.mod file is found by walking up the directory tree), the "go" subcommand
is automatically selected as the default. This makes "tai" convenient to invoke
in Go projects without explicitly specifying the subcommand each time, and
every such invocation runs the multi-loop goal mechanism.
`

var GoCommand = Command{
	Defs: []any{
		modes.ForProduction(),
		func(
			provider gotools.PartsProvider,
		) codetypes.PartsProvider {
			return provider
		},
		func(
			comps pipeline.CodesComponents,
			feedback pipeline.GoalFeedback,
			summaries pipeline.GoalLoopSummaries,
		) pipeline.SystemPrompt {
			return pipeline.GoalSystemPromptText(comps, feedback, summaries)
		},
	},
	Main: func(
		goalRun pipeline.GoalRun,
		output Output,
		tap debugs.Tap,
		repl Repl,
		noHuman NoHuman,
	) {
		if bool(repl) && !bool(noHuman) {
			tap(context.Background(), "repl", map[string]any{})
			return
		}
		goalRun(context.Background(), output)
	},
}

type InGoModule bool

func (Module) InGoModule() InGoModule {
	dir, err := os.Getwd()
	if err != nil {
		return false
	}
	return InGoModule(dirHasGoModule(dir))
}

// dirHasGoModule walks up the directory tree from dir looking for a go.mod
// file. It returns true if one is found, false if the filesystem root is
// reached without finding one.
func dirHasGoModule(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
