package codes

import (
	"context"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/flags"
)

const TheoryOfCodesBlockBindings = `
CodesBlockBindings is a distinct named type embedding blocks.BlockBindings so that
dscope resolves it independently from other modules' BlockBindings providers (e.g.,
the ai command's AIBlockBindings). Without a separate type, the codes module and
any other module providing blocks.BlockBindings would conflict in the dscope scope.
This mirrors the AIBlockBindings pattern in the ai command, ensuring every module
that provides block bindings uses its own named type to avoid dscope type conflicts.

The codes module reuses blocks.CommonBlockBindings for the shell and continue block
kinds, prepending its codes-specific bindings (change, finish, request-context) and
appending summary. This eliminates duplicate shell and continue binding construction
across modules.
`

// CodesBlockBindings is the block bindings type for the codes module. It embeds
// blocks.BlockBindings as an anonymous struct field so that dscope can resolve
// it independently from other modules' BlockBindings providers. Method
// promotion eliminates the need for explicit delegation methods.
// See TheoryOfCodesBlockBindings.
type CodesBlockBindings struct {
	blocks.BlockBindings
}

func (Module) BlockBindings(
	diffHandler codetypes.DiffHandler,
	dynamicContext DynamicContext,
	apply Apply,
	flagShell flags.Shell,
) CodesBlockBindings {
	var bindings blocks.BlockBindings

	// Change block: prompt always included (from diff handler, which
	// includes BlockFormatSystemPrompt and ChangeBlockSystemPrompt).
	// Processing is conditional on the apply flag.
	if bool(apply) {
		bindings = append(bindings, blocks.BlockBinding{
			Kind:          "change",
			PromptSection: diffHandler.SystemPrompt(),
			Process: func(ctx context.Context, pctx *blocks.ProcessContext) blocks.ProcessResult {
				newPs, err := applyChangeBlocks(pctx.ParserState, pctx.Root)
				return blocks.ProcessResult{
					ParserState: newPs,
					Err:         err,
				}
			},
		})
	} else {
		bindings = append(bindings, blocks.BlockBinding{
			Kind:           "change",
			PromptSection:  diffHandler.SystemPrompt(),
			ProcessingPath: "applyChangeBlocks (disabled by -no-apply)",
		})
	}

	// Finish block: informational, not processed.
	bindings = append(bindings, blocks.BlockBinding{
		Kind:           "finish",
		PromptSection:  blocks.FinishBlockSystemPrompt,
		ProcessingPath: "informational",
	})

	// Request-context block: conditional on dynamicContext.
	// Processed before shell/continue so fetched context is available
	// for the next generation round.
	if bool(dynamicContext) {
		bindings = append(bindings, blocks.BlockBinding{
			Kind:          "request-context",
			PromptSection: blocks.RequestContextSystemPrompt,
			MaxRounds:     maxRequestContextRounds,
			Process: func(ctx context.Context, pctx *blocks.ProcessContext) blocks.ProcessResult {
				state, newPs, hasRC, err := blocks.ProcessRequestContextBlocks(
					pctx.ParserState, ctx, pctx.Root, pctx.HttpClient, pctx.State,
				)
				return blocks.ProcessResult{
					ParserState: newPs,
					State:       state,
					Continue:    hasRC,
					Err:         err,
				}
			},
		})
	}

	// Common block kinds: shell (conditional on flagShell) and continue.
	// Reused from blocks.CommonBlockBindings so that shell and continue
	// configuration is shared across all generation commands.
	// See TheoryOfCommonBlockBindings in blocks/handler.go.
	bindings = append(bindings, blocks.CommonBlockBindings(bool(flagShell))...)

	// Summary block: processed in runPhaseWithRetry for completion detection
	// and round statistics, not in the main binding loop.
	bindings = append(bindings, blocks.BlockBinding{
		Kind:           "summary",
		PromptSection:  blocks.SummaryBlockSystemPrompt,
		ProcessingPath: "runPhaseWithRetry",
	})

	return CodesBlockBindings{bindings}
}
