package components

import (
	"context"

	"github.com/reusee/tai/blocks"
)

const TheoryOfCommonComponents = `
CommonComponents returns the components shared across all generation commands:
shell (conditional on the shell flag) and continue. These are generic,
side-effect-free components that any generation pipeline may use regardless of
whether it performs code modification or dynamic context fetching. Commands
that need additional components (e.g., change for code generation, ingest for
dynamic context, summary for attempt statistics, read-only files for
prompt-only rules) prepend or append their specific components to this common
set. The common components are constructed once and reused by both the ai
command (via AIComponents) and the pipeline (via CodesComponents), ensuring
that shell and continue components are consistently configured across all
generation pipelines.

The common set itself carries no disabled-blocks notices: a caller that
disables a common kind (shell without the flag) or excludes one (the ai
command excludes continue) announces it through its own
DisabledBlocksComponent, so each prompt carries one complete notice. See
TheoryOfDisabledBlocks.

The common components carry no reminder text of their own; the late reminder
role belongs to the verbatim system prompt restate (see TheoryOfComponents).

Components carry no per-kind generation bounds: a session may chain any
number of shell, continue, go-test, go-src, or ingest generations, so a
model can run as long as the task requires. Run-duration control belongs to
the caller, not the component layer — pipeline.RunOptions.MaxGenerations
caps the total generations of a whole run (0 means unlimited) — and an
unattended operator terminates the process when they choose. The accepted
trade-off is that a runaway model consumes tokens until the caller stops it;
legitimate long workflows are never aborted mid-task by an internal bound.
`

func CommonComponents(shell bool) ComponentSet {
	var comps ComponentSet
	if shell {
		comps = append(comps, Component{
			Kind:          "shell",
			PromptSection: blocks.ShellBlockSystemPrompt,
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, err := blocks.ProcessShellBlocks(pctx.Blocks, ctx)
				return ProcessResult{Parts: parts, Err: err}
			},
		})
	}
	comps = append(comps, Component{
		Kind:          "continue",
		PromptSection: blocks.ContinueBlockSystemPrompt,
		Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
			parts := blocks.ProcessContinueBlocks(pctx.Blocks)
			return ProcessResult{Parts: parts}
		},
	})
	return comps
}
