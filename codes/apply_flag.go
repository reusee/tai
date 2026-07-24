package codes

import (
	"fmt"
	"os"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/flags"
)

const TheoryOfImmediateApply = `
Immediate apply enables change blocks parsed from streamed model output to be
applied to the working tree as soon as a generation phase completes, rather than
buffering all output and applying after the full generation session finishes.
This reuses the ParserState decorator (shared with dynamic context) to intercept
change blocks from model output. ParserState is activated when either dynamic
context or immediate apply is enabled, because both features parse structured
blocks from streamed output. An apply error aborts generation immediately so the
user can inspect the partial state and the failing hunk rather than continuing
to produce changes that build on a broken foundation.
Immediate apply is enabled by default; the -no-apply flag disables it so change
blocks are not applied to the working tree during generation.
`

// Apply controls whether change blocks are applied to the working tree
// immediately as they are parsed from model output during generation.
// When true, ParserState is activated to intercept change blocks, and each
// complete change block is applied via applyHunk after a generation phase.
// An apply error aborts generation. See TheoryOfImmediateApply.
type Apply bool

func (Module) Apply() Apply {
	return true
}

var _ flags.Flag = Apply(true)

func (a Apply) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	switch key {
	case "-apply":
		return Apply(true), args, nil
	case "-no-apply":
		return Apply(false), args, nil
	}
	panic("key not handle: " + key)
}

func (a Apply) Keys() map[string]string {
	return map[string]string{
		"-apply":    "Apply change blocks to the working tree during generation",
		"-no-apply": "Do not apply change blocks during generation",
	}
}

// applyChangeBlocks pops all complete change blocks from parserState and
// applies them to the working tree via applyHunk. It returns a new
// *ParserState with the change blocks removed and an error if any block is
// unparseable or if applyHunk fails. The original parserState is not modified.
// See TheoryOfImmediateApply and TheoryOfParserState.
func applyChangeBlocks(parserState *blocks.ParserState, root *os.Root) (*blocks.ParserState, error) {
	changeBlocks, newParserState := parserState.PopBlocksByKind("change")
	for _, block := range changeBlocks {
		h, parsedOk := blocks.ParseChangeBlock(block)
		if !parsedOk {
			return newParserState, fmt.Errorf("unparseable change block with boundary %s", block.Boundary)
		}
		if err := applyHunk(root, h); err != nil {
			return newParserState, fmt.Errorf("apply hunk %s %s: %w", h.Op, h.Target, err)
		}
	}
	return newParserState, nil
}
