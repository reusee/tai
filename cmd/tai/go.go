package main

import (
	"context"
	"os"

	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/gotools"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pathutil"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/pipeline/codetypes"
)

const TheoryOfGoModuleDefault = `
GoModuleCommand is the default command inside a Go module (see
TheoryOfCommandAutoDetection): it selects the Go parts provider
(gotools.PartsProvider) and always runs goal mode (pipeline.GoalRun):
repeated fresh generation loops until a change-free loop emits a done
block — the run's only exit — with each loop's outcome carried into
the next loop's system prompt as pipeline.GoalFeedback, alongside the
summaries of all previous loops (pipeline.GoalLoopSummaries). The
system prompt is forked through pipeline.GoalSystemPromptText, which
composes the base codes prompt, the goal system prompt, and the
component sections, appending the previous-loop summaries and the loop
feedback at the end. Generation and review stream to os.Stdout;
verdicts and failure notes are reported as pipeline events (EventGoal)
through the goal event observer, which the TUI forks to its Events tab;
the command-line path writes them to the command Output writer. The
-repl flag enables a REPL mode that taps the debugs infrastructure
without running generation, useful for interactive debugging.
`

var GoModuleCommand = apps.New("go_module", "",
	func(
		goalRun pipeline.GoalRun,
		output Output,
		tap debugs.Tap,
		repl Repl,
	) {
		if bool(repl) {
			tap(context.Background(), "repl", map[string]any{})
			return
		}
		goalRun(context.Background(), output)
	},
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
)

type InGoModule bool

func (Module) InGoModule() InGoModule {
	dir, err := os.Getwd()
	if err != nil {
		return false
	}
	_, ok := pathutil.FindGoModuleRoot(dir)
	return InGoModule(ok)
}
