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
`

// CommonComponents returns the components shared across all generation
// commands: shell (conditional on the shell flag) and continue. Commands
// that need additional components prepend or append their specific
// components to this common set.
// See TheoryOfCommonComponents.
func CommonComponents(shell bool) ComponentSet {
	var comps ComponentSet
	if shell {
		comps = append(comps, Component{
			Kind:          "shell",
			PromptSection: blocks.ShellBlockSystemPrompt,
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, newPs, err := blocks.ProcessShellBlocks(pctx.ParserState)
				return ProcessResult{ParserState: newPs, Parts: parts, Err: err}
			},
		})
	}
	comps = append(comps, Component{
		Kind:          "continue",
		PromptSection: blocks.ContinueBlockSystemPrompt,
		Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
			parts, newPs := blocks.ProcessContinueBlocks(pctx.ParserState)
			return ProcessResult{ParserState: newPs, Parts: parts}
		},
	})
	return comps
}
