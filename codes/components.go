package codes

import (
	"context"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gocodes"
)

const TheoryOfCodesComponents = `
CodesComponents is a distinct named type embedding components.ComponentSet so that
dscope resolves it independently from other modules' ComponentSet providers (e.g.,
the ai command's AIComponents).

The codes module reuses components.CommonComponents for the shell and continue
component kinds, prepending its codes-specific components (change, go-test,
request-context) and appending summary, read-only files (prompt-only),
mandatory planning (prompt-only, conditional), and extra system prompt
(prompt-only).

The unified block format prompt (blocks.BlockFormatSystemPrompt) is included
as the first prompt-only component: every block-using component set must carry
it, and the kind prompts describe only their kind-specific semantics without
restating the heredoc format. See blocks.TheoryOfBlockFormatGeneral.

The go-test component runs Go tests after change blocks are applied. Test
output is fed back to the model only when tests fail, producing Parts that
trigger a new round for debugging with MaxRounds bounding the test-fix loop.
When tests pass, no Parts are returned and no round is triggered by go-test.
However, the go-test component provides BackgroundParts — a pass confirmation
message — that ProcessComponents includes in the combined output only when
another component triggers a new round (e.g., continue). This ensures the
model knows the tests passed and does not re-emit go-test blocks in
subsequent rounds, preventing unnecessary test reruns. When no component
triggers, BackgroundParts are discarded. The go-test component is placed
after change so tests run against the updated source, and before summary so
test output is available for the next round.

Read-only files and mandatory planning are prompt-only Components: they
contribute system prompt sections without defining a block kind or processing
blocks.

ExtraSystemPrompt is also a prompt-only Component. Change, go-test, and
request-context components carry RestatePrompt fields — short critical reminders
that reinforce block format rules. Restate prompts are placed at the end of
the user prompt via ComponentSet.UserPromptParts(), not in the system prompt,
so they are the last content the model reads before generating.

The summary component carries a RestatePrompt (SummaryBlockRestatePrompt)
that reinforces the requirement to emit a summary block in every response as
the round completion signal. The summary block is the sole completion signal:
the generation loop checks for its presence to distinguish a normally ended
round from truncated output.
`

const TheoryOfFamilyExtraSystemPrompt = `
Family-specific extra system prompts extend the generic extra_system_prompt
mechanism with prompts keyed by the model family. The top-level
family_extra_system_prompt applies to every generation command (codes, ai,
next); the go.family_extra_system_prompt applies only when the codes
generation pipeline is active (go, any, goal), mirroring the split between
extra_system_prompt and go.extra_system_prompt. Prompts are selected by the
family of the resolved default generator (generators.Spec.Family) and are
appended as prompt-only components after the generic extra prompts, so a
family-specific prompt refines or extends the generic instructions without
replacing them. The family is resolved through the generators.ModelFamily
provider, which derives the family from the resolved default generator, so
no customization is needed.
`

// CodesComponents is the component set type for the codes module. It embeds
// components.ComponentSet as an anonymous struct field so that dscope can
// resolve it independently from other modules' ComponentSet providers.
// See TheoryOfCodesComponents.
type CodesComponents struct {
	components.ComponentSet
}

func (Module) CodesComponents(
	extra flags.ExtraSystemPrompt,
	goExtra gocodes.ExtraSystemPrompt,
	familyExtra flags.FamilyExtraSystemPrompt,
	goFamilyExtra gocodes.FamilyExtraSystemPrompt,
	modelFamily generators.ModelFamily,
	dynamicContext DynamicContext,
	apply flags.Apply,
	plan flags.Plan,
	flagShell flags.Shell,
	applyChangeBlocks changes.ApplyChangeBlocks,
) CodesComponents {
	var comps components.ComponentSet

	// The unified block format prompt is the first prompt-only component:
	// it teaches the heredoc-delimited block format that every kind prompt
	// below assumes. Kind prompts describe only their kind-specific
	// semantics without restating the format. See
	// blocks.TheoryOfBlockFormatGeneral.
	comps = append(comps, components.Component{
		PromptSection: blocks.BlockFormatSystemPrompt,
		RestatePrompt: blocks.BlockFormatRestatePrompt,
	})

	// Change component: prompt always included (from the change block
	// prompt, which describes only the change-kind semantics; the unified
	// block format is the first component above). Processing is
	// conditional on the apply flag. See TheoryOfCodesComponents.
	if bool(apply) {
		comps = append(comps, components.Component{
			Kind:          "change",
			PromptSection: changes.ChangeBlockPrompt,
			RestatePrompt: changes.ChangeBlockRestatePrompt(),
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				err := applyChangeBlocks(pctx.Blocks, pctx.Root)
				return components.ProcessResult{Err: err}
			},
		})
	} else {
		// Change blocks are not applied when -no-apply is set; the
		// prompt is still included so the model knows the format.
		comps = append(comps, components.Component{
			Kind:          "change",
			PromptSection: changes.ChangeBlockPrompt,
			RestatePrompt: changes.ChangeBlockRestatePrompt(),
		})
	}

	// Go-test component: run Go tests after change blocks are applied.
	// Test output is fed back to the model only when tests fail,
	// producing Parts that trigger a new round for debugging. When tests
	// pass, BackgroundParts carry a pass confirmation that
	// ProcessComponents includes when another component triggers a new
	// round, so the model knows tests passed and does not re-emit
	// go-test blocks. MaxRounds bounds the test-fix loop. Placed after
	// change so tests run against updated source, and before summary so
	// test output is available for the next round.
	// See TheoryOfCodesComponents and TheoryOfGoTestBlocks.
	comps = append(comps, components.Component{
		Kind:          "go-test",
		PromptSection: blocks.GoTestBlockSystemPrompt,
		RestatePrompt: blocks.GoTestBlockRestatePrompt,
		MaxRounds:     maxGoTestRounds,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			parts, failed, err := blocks.ProcessGoTestBlocks(pctx.Blocks, ctx)
			result := components.ProcessResult{
				Err: err,
			}
			if failed {
				// Only feed test output to the next round when tests fail,
				// so the model can debug the failures. Parts trigger a new
				// round for debugging.
				result.Parts = parts
			} else {
				// Tests passed. Provide BackgroundParts so that when
				// another component (e.g., continue) triggers a new
				// round, the model is informed that tests passed and
				// does not re-emit go-test blocks. When no component
				// triggers, BackgroundParts are discarded because there
				// is no next round to carry them.
				result.BackgroundParts = []generators.Part{
					generators.Text("Go tests passed. All test commands succeeded.\n\n"),
				}
			}
			return result
		},
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
				state, hasRC, err := blocks.ProcessRequestContextBlocks(
					pctx.Blocks, ctx, pctx.Root, pctx.HttpClient, pctx.State,
				)
				result := components.ProcessResult{
					Err: err,
				}
				// Only set State when request-context blocks were
				// found and fetched content was appended, so that
				// result.State != nil reliably signals a state
				// modification that triggers a new round.
				if hasRC {
					result.State = state
				}
				return result
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
	// RestatePrompt reinforces the requirement to emit a summary block in
	// every response as the round completion signal. See
	// TheoryOfCodesComponents.
	comps = append(comps, components.Component{
		Kind:          "summary",
		PromptSection: blocks.SummaryBlockSystemPrompt,
		RestatePrompt: blocks.SummaryBlockRestatePrompt,
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
	// Each entry is added as a separate prompt-only Component so that
	// multiple config sources are all included.
	// Unified under the Component framework so all prompt contributions
	// are assembled through comps.PromptSections(). See TheoryOfCodesComponents.
	for _, prompt := range extra {
		if prompt != "" {
			comps = append(comps, components.Component{
				PromptSection: prompt,
			})
		}
	}

	// Go-specific extra system prompt from configuration
	// (go.extra_system_prompt): prompt-only Component, appended after the
	// top-level extra prompts so the go project context is introduced
	// whenever the codes generation pipeline is active (go, any, goal
	// commands). The ai command uses AIComponents and is unaffected.
	// See gocodes.ExtraSystemPrompt.
	for _, prompt := range goExtra {
		if prompt != "" {
			comps = append(comps, components.Component{
				PromptSection: prompt,
			})
		}
	}

	// Family-specific extra system prompts: top-level and go-specific
	// prompts keyed by the model family. The family is resolved from the
	// scope via generators.ModelFamily; when the family matches a key,
	// the corresponding prompts are appended as prompt-only components
	// after the generic extra prompts. See
	// TheoryOfFamilyExtraSystemPrompt.
	for _, prompt := range familyExtra[string(modelFamily)] {
		if prompt != "" {
			comps = append(comps, components.Component{
				PromptSection: prompt,
			})
		}
	}
	for _, prompt := range goFamilyExtra[string(modelFamily)] {
		if prompt != "" {
			comps = append(comps, components.Component{
				PromptSection: prompt,
			})
		}
	}

	return CodesComponents{comps}
}
