package codes

import (
	"context"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
)

const TheoryOfCodesComponents = `
CodesComponents is a distinct named type embedding components.ComponentSet so that
dscope resolves it independently from other modules' ComponentSet providers (e.g.,
the ai command's AIComponents). Without a separate type, the codes module and
any other module providing components.ComponentSet would conflict in the dscope scope.
This mirrors the AIComponents pattern in the ai command, ensuring every module
that provides components uses its own named type to avoid dscope type conflicts.

The codes module reuses components.CommonComponents for the shell and continue
component kinds, prepending its codes-specific components (change, finish,
request-context) and appending read-only files (prompt-only), mandatory planning
(prompt-only, conditional), and summary. This eliminates duplicate shell and
continue component construction across modules.

Read-only files and mandatory planning are prompt-only Components: they
contribute system prompt sections without defining a block kind or processing
blocks. This demonstrates the Component concept's unification of prompt-only
mechanisms with block processing mechanisms under a single framework.
`

// CodesComponents is the component set type for the codes module. It embeds
// components.ComponentSet as an anonymous struct field so that dscope can
// resolve it independently from other modules' ComponentSet providers.
// See TheoryOfCodesComponents.
type CodesComponents struct {
	components.ComponentSet
}

func (Module) CodesComponents(
	diffHandler codetypes.DiffHandler,
	dynamicContext DynamicContext,
	apply Apply,
	plan Plan,
	flagShell flags.Shell,
) CodesComponents {
	var comps components.ComponentSet

	// Change component: prompt always included (from diff handler, which
	// includes BlockFormatSystemPrompt and ChangeBlockSystemPrompt).
	// Processing is conditional on the apply flag.
	if bool(apply) {
		comps = append(comps, components.Component{
			Kind:          "change",
			PromptSection: diffHandler.SystemPrompt(),
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				newPs, err := applyChangeBlocks(pctx.ParserState, pctx.Root)
				return components.ProcessResult{
					ParserState: newPs,
					Err:         err,
				}
			},
		})
	} else {
		comps = append(comps, components.Component{
			Kind:           "change",
			PromptSection:  diffHandler.SystemPrompt(),
			ProcessingPath: "applyChangeBlocks (disabled by -no-apply)",
		})
	}

	// Finish component: informational, not processed.
	comps = append(comps, components.Component{
		Kind:           "finish",
		PromptSection:  blocks.FinishBlockSystemPrompt,
		ProcessingPath: "informational",
	})

	// Request-context component: conditional on dynamicContext.
	// Processed before shell/continue so fetched context is available
	// for the next generation round.
	if bool(dynamicContext) {
		comps = append(comps, components.Component{
			Kind:          "request-context",
			PromptSection: blocks.RequestContextSystemPrompt,
			MaxRounds:     maxRequestContextRounds,
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				state, newPs, hasRC, err := blocks.ProcessRequestContextBlocks(
					pctx.ParserState, ctx, pctx.Root, pctx.HttpClient, pctx.State,
				)
				return components.ProcessResult{
					ParserState: newPs,
					State:       state,
					Continue:    hasRC,
					Err:         err,
				}
			},
		})
	}

	// Common components: shell (conditional on flagShell) and continue.
	// Reused from components.CommonComponents so that shell and continue
	// configuration is shared across all generation commands.
	// See TheoryOfCommonComponents in components/common_components.go.
	comps = append(comps, components.CommonComponents(bool(flagShell))...)

	// Summary component: processed in runPhaseWithRetry for completion detection
	// and round statistics, not in the main component loop.
	comps = append(comps, components.Component{
		Kind:           "summary",
		PromptSection:  blocks.SummaryBlockSystemPrompt,
		ProcessingPath: "runPhaseWithRetry",
	})

	// Read-only files: prompt-only component, no block kind.
	comps = append(comps, components.Component{
		PromptSection: ReadOnlyFilesSystemPrompt,
	})

	// Mandatory planning: prompt-only component, conditional on plan.
	if bool(plan) {
		comps = append(comps, components.Component{
			PromptSection: MandatoryPlanningSystemPrompt,
		})
	}

	return CodesComponents{comps}
}
