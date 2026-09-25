package components

import (
	"context"

	"github.com/reusee/tai/blocks"
)

const TheoryOfCommonComponents = `
CommonComponents returns the components shared across all generation commands:
shell and continue. These are generic, side-effect-free components that any
generation pipeline may use regardless of whether it performs code
modification or dynamic context fetching. Commands that need additional
components (e.g., change for code generation, ingest for dynamic context,
summary for attempt statistics, read-only files for prompt-only rules)
prepend or append their specific components to this common set. The common
components are constructed once and reused by both the ai command (via
AIComponents) and the pipeline (via CodesComponents), ensuring that shell and
continue components are consistently configured across all generation
pipelines.

Shell processing is enabled by the shell flag or by a configured command
allowlist, which is the user's own decision to let the model run the listed
commands: the component carries the matching policy prompt and enforces the
allowlist on every block. An unconfigured allowlist keeps the previous
behavior, so a session without either switch is unchanged. See
blocks.TheoryOfShellAllowlist.

Components carry no per-kind generation bounds: a session may chain any
number of shell, continue, go-test, go-src, or ingest generations, so a
model can run as long as the task requires. Run-duration control belongs to
the caller, not the component layer — pipeline.RunOptions.MaxGenerations
caps the total generations of a whole run (0 means unlimited) — and an
unattended operator terminates the process when they choose. The accepted
trade-off is that a runaway model consumes tokens until the caller stops it;
legitimate long workflows are never aborted mid-task by an internal bound.
`

// CommonComponents returns the components shared by every generation
// command: shell and continue. The shell component is present when shell
// block processing is enabled — the shell flag, or a configured command
// allowlist, which is the user's own decision to let the model run the
// listed commands (see blocks.TheoryOfShellAllowlist). The component
// carries the policy prompt and enforces the allowlist on every block.
// See TheoryOfCommonComponents.
func CommonComponents(shell bool, allowed blocks.AllowedShellCommands) ComponentSet {
	var comps ComponentSet
	if allowed.ShellEnabled(shell) {
		comps = append(comps, Component{
			Kind:          "shell",
			PromptSection: blocks.ShellBlockPrompt(allowed),
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, err := blocks.ProcessShellBlocks(pctx.Blocks, ctx, allowed)
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
