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
that need additional components (e.g., change for code generation,
request-context for dynamic context, finish and summary for round statistics,
read-only files for prompt-only rules) prepend or append their specific
components to this common set. The common components are constructed once and
reused by both the ai command (via AIComponents) and the codes module (via
CodesComponents), ensuring that shell and continue components are consistently
configured across all generation pipelines.

Shell and continue components include RestatePrompt fields that provide short
critical reminders reinforcing the block format rules at the end of the system
prompt, improving the model's adherence to the boundary-delimited block format
across all generation commands.

The shell component is bounded by maxShellRounds and the continue component by
maxContinueRounds. Both produce output that is fed back to the model, so a
model that keeps emitting blocks of either kind would otherwise loop forever in
unattended operation. The bounds are deliberately generous — shell commands are
often part of legitimate iterative workflows, and continue blocks are the
transport for multi-round task decomposition (plan mode) — so normal use is
unaffected while a runaway model is stopped with a clear error. Exceeding a
bound aborts the run; the goal command surfaces the error per loop and starts
a fresh loop from the current filesystem state.
`

// maxShellRounds bounds the number of rounds the shell component may
// trigger. Shell output is fed back to the model, so a model that keeps
// emitting shell blocks would otherwise loop forever in unattended
// operation. Exceeding the bound aborts the run with a clear error.
// See TheoryOfCommonComponents.
//
// maxContinueRounds bounds the number of rounds the continue component
// may trigger. Continue blocks are the transport for multi-round task
// decomposition (plan mode); the bound is deliberately generous so
// legitimate large tasks are unaffected, while a runaway model that
// keeps emitting continue blocks cannot loop forever in unattended
// operation. See TheoryOfCommonComponents.
const (
	maxShellRounds    = 50
	maxContinueRounds = 200
)

func CommonComponents(shell bool) ComponentSet {
	var comps ComponentSet
	if shell {
		comps = append(comps, Component{
			Kind:          "shell",
			PromptSection: blocks.ShellBlockSystemPrompt,
			RestatePrompt: blocks.ShellBlockRestatePrompt,
			MaxRounds:     maxShellRounds,
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, err := blocks.ProcessShellBlocks(pctx.Blocks, ctx)
				return ProcessResult{Parts: parts, Err: err}
			},
		})
	}
	comps = append(comps, Component{
		Kind:          "continue",
		PromptSection: blocks.ContinueBlockSystemPrompt,
		RestatePrompt: blocks.ContinueBlockRestatePrompt,
		MaxRounds:     maxContinueRounds,
		Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
			parts := blocks.ProcessContinueBlocks(pctx.Blocks)
			return ProcessResult{Parts: parts}
		},
	})
	return comps
}
