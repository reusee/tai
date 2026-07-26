package changes

import (
	"fmt"
	"os"

	"github.com/reusee/tai/blocks"
)

// ApplyChangeBlocks pops all complete change blocks from parserState and
// applies them to the working tree via ApplyHunk. It returns a new
// *ParserState with the change blocks removed and an error if any block is
// unparseable or if ApplyHunk fails. The original parserState is not modified.
// See TheoryOfImmediateApply and TheoryOfParserState.
func ApplyChangeBlocks(parserState *blocks.ParserState, root *os.Root) (*blocks.ParserState, error) {
	changeBlocks, newParserState := parserState.PopBlocksByKind("change")
	for _, block := range changeBlocks {
		h, parsedOk := ParseChangeBlock(block)
		if !parsedOk {
			return newParserState, fmt.Errorf("unparseable change block with boundary %s", block.Boundary)
		}
		if err := ApplyHunk(root, h); err != nil {
			return newParserState, fmt.Errorf("apply hunk %s %s: %w", h.Op, h.Target, err)
		}
	}
	return newParserState, nil
}