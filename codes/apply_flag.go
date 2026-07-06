package codes

import (
	"fmt"
	"os"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/cmds"
)

const TheoryOfImmediateApply = `
Immediate apply enables change blocks parsed from streamed model output to be
applied to the working tree as soon as a generation phase completes, rather than
buffering all output and applying after the full generation session finishes.
This reuses the BlockState decorator (shared with dynamic context) to intercept
change blocks from model output. BlockState is activated when either dynamic
context or immediate apply is enabled, because both features parse structured
blocks from streamed output. An apply error aborts generation immediately so the
user can inspect the partial state and the failing hunk rather than continuing
to produce changes that build on a broken foundation.
Immediate apply is enabled by default; the -no-apply flag disables it so change
blocks are not applied to the working tree during generation.
`

// Apply controls whether change blocks are applied to the working tree
// immediately as they are parsed from model output during generation.
// When true, BlockState is activated to intercept change blocks, and each
// complete change block is applied via applyHunk after a generation phase.
// An apply error aborts generation. See TheoryOfImmediateApply.
type Apply bool

var applyFlag Apply = true

func init() {
	cmds.Define("-no-apply", cmds.Func(func() {
		applyFlag = false
	}).Desc("disable immediate apply of change blocks"))
}

func (Module) Apply() Apply {
	return applyFlag
}

// applyChangeBlocks pops all complete change blocks from blockState and
// applies them to the working tree via applyHunk. It returns an error if any
// block is unparseable or if applyHunk fails, so the caller can abort
// generation. See TheoryOfImmediateApply.
func applyChangeBlocks(blockState *blocks.BlockState, root *os.Root) error {
	for _, block := range blockState.PopBlocksByKind("change") {
		h, parsedOk := blocks.ParseChangeXMLBody(block.Body)
		if !parsedOk {
			return fmt.Errorf("unparseable change block with boundary %s", block.Boundary)
		}
		if err := applyHunk(root, h); err != nil {
			return fmt.Errorf("apply hunk %s %s: %w", h.Op, h.Target, err)
		}
	}
	return nil
}