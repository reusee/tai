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

The codes module reuses components.CommonComponents for the shell and continue
component kinds, prepending its codes-specific components (change, go-test,
finish, request-context) and appending read-only files (prompt-only), mandatory
planning (prompt-only, conditional), and summary. This eliminates duplicate
shell and continue component construction across modules.

The go-test component runs Go tests after change blocks are applied. Test
output is fed back to the model only when tests fail, triggering a new round
for debugging with MaxRounds bounding the test-fix loop. When tests pass, no
output is returned and no round is triggered by go-test, so other mechanisms
(e.g., continue blocks) are unaffected. The go-test component is placed after
change so tests run against the updated source, and before finish so test
failures preempt the completion signal.

Read-only files and mandatory planning are prompt-only Components: they
contribute system prompt sections without defining a block kind or processing
blocks. This demonstrates the Component concept's unification of prompt-only
mechanisms with block processing mechanisms under a single framework.

ExtraSystemPrompt is also a prompt-only Component, unifying the config-derived
extra prompt into the same assembly mechanism. Change, go-test, finish, and
request-context components carry RestatePrompt fields — short critical reminders
that reinforce block format rules, assembled via ComponentSet.RestatePrompts()
separately from the main PromptSections. This brings the previously orphaned
restate prompt constants (ChangeBlockRestatePrompt, GoTestBlockRestatePrompt,
FinishBlockRestatePrompt, RequestContextRestatePrompt) and the
DiffHandler.RestatePrompt() method under the Component framework, making them
functional for the first time.
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
	codeProvider codetypes.CodeProvider,
	extra ExtraSystemPrompt,
	dynamicContext DynamicContext,
	apply Apply,
	plan Plan,
	flagShell flags.Shell,
) CodesComponents {
	var comps components.ComponentSet

	// Change component: prompt always included (from diff handler, which
	// includes BlockFormatSystemPrompt and ChangeBlockSystemPrompt).
	// Processing is conditional on the apply flag.
	// RestatePrompt carries the change block restate prompt from the diff
	// handler, making the previously orphaned RestatePrompt() method
	// functional. See TheoryOfCodesComponents.
	if bool(apply) {
		comps = append(comps, components.Component{
			Kind:          "change",
			PromptSection: diffHandler.SystemPrompt(),
			RestatePrompt: diffHandler.RestatePrompt(),
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
			RestatePrompt:  diffHandler.RestatePrompt(),
			ProcessingPath: "applyChangeBlocks (disabled by -no-apply)",
		})
	}

	// Go-test component: run Go tests after change blocks are applied.
	// Test output is fed back to the model only when tests fail,
	// triggering a new round for debugging. When tests pass, no output
	// is returned and no round is triggered, so other mechanisms (e.g.,
	// continue blocks) are unaffected. MaxRounds bounds the test-fix
	// loop. Placed after change so tests run against updated source,
	// and before finish so test failures preempt the completion signal.
	// See TheoryOfCodesComponents.
	comps = append(comps, components.Component{
		Kind:          "go-test",
		PromptSection: blocks.GoTestBlockSystemPrompt,
		RestatePrompt: blocks.GoTestBlockRestatePrompt,
		MaxRounds:     maxGoTestRounds,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			parts, newPs, failed, err := blocks.ProcessGoTestBlocks(pctx.ParserState, ctx)
			result := components.ProcessResult{
				ParserState: newPs,
				Err:         err,
			}
			// Only feed test output to the next round when tests fail,
			// so the model can debug the failures. When tests pass, no
			// output is returned and no round is triggered by go-test,
			// allowing other mechanisms (e.g., continue blocks) to
			// function independently. See TheoryOfCodesComponents.
			if failed {
				result.Parts = parts
				result.Continue = true
			}
			return result
		},
	})

	// Finish component: informational, not processed.
	// RestatePrompt carries the finish block restate prompt.
	comps = append(comps, components.Component{
		Kind:           "finish",
		PromptSection:  blocks.FinishBlockSystemPrompt,
		RestatePrompt:  blocks.FinishBlockRestatePrompt,
		ProcessingPath: "informational",
	})

	// Request-context component: conditional on dynamicContext.
	// Processed before shell/continue so fetched context is available
	// for the next generation round.
	// RestatePrompt carries the request-context restate prompt.
	if bool(dynamicContext) {
		comps = append(comps, components.Component{
			Kind:          "request-context",
			PromptSection: blocks.RequestContextSystemPrompt,
			RestatePrompt: blocks.RequestContextRestatePrompt,
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

	// Extra system prompt from configuration: prompt-only Component.
	// Unified under the Component framework so all prompt contributions
	// are assembled through comps.PromptSections(). See TheoryOfCodesComponents.
	if string(extra) != "" {
		comps = append(comps, components.Component{
			PromptSection: string(extra),
		})
	}

	return CodesComponents{comps}
}
