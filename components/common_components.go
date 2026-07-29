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
configured across all generation pipelines and eliminating the duplicate
component construction that previously existed in each module.

Shell and continue components include RestatePrompt fields that provide short
critical reminders reinforcing the block format rules at the end of the system
prompt, improving the model's adherence to the boundary-delimited block format
across all generation commands.
`

func CommonComponents(shell bool) ComponentSet {
	var comps ComponentSet
	if shell {
		comps = append(comps, Component{
			Kind:          "shell",
			PromptSection: blocks.ShellBlockSystemPrompt,
			RestatePrompt: blocks.ShellBlockRestatePrompt,
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, err := blocks.ProcessShellBlocks(pctx.Blocks)
				return ProcessResult{Parts: parts, Err: err}
			},
		})
	}
	comps = append(comps, Component{
		Kind:          "continue",
		PromptSection: blocks.ContinueBlockSystemPrompt,
		RestatePrompt: blocks.ContinueBlockRestatePrompt,
		Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
			parts := blocks.ProcessContinueBlocks(pctx.Blocks)
			return ProcessResult{Parts: parts}
		},
	})
	return comps
}
