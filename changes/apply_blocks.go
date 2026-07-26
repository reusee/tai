package changes

import (
	"fmt"
	"os"

	"github.com/reusee/tai/blocks"
)

// ApplyChangeBlocks pops all complete change blocks from parserState and
// applies them to the working tree via ApplyChangeBlock. It returns a new
// *ParserState with the change blocks removed and an error if any block is
// unparseable or if ApplyChangeBlock fails. The original parserState is not
// modified. See TheoryOfImmediateApply and TheoryOfParserState.
func ApplyChangeBlocks(parserState *blocks.ParserState, root *os.Root) (*blocks.ParserState, error) {
	return ApplyChangeBlocksStore(parserState, NewRootStore(root))
}

// ApplyChangeBlocksStore pops all complete change blocks from parserState and
// applies them to the given FileStore via ApplyChangeBlockStore. It returns a
// new *ParserState with the change blocks removed and an error if any block is
// unparseable or if ApplyChangeBlockStore fails. The original parserState is
// not modified. When the store is a MemoryStore, changes are buffered in
// memory and only written to disk on Flush. See TheoryOfInMemoryApply.
func ApplyChangeBlocksStore(parserState *blocks.ParserState, store FileStore) (*blocks.ParserState, error) {
	changeBlocks, newParserState := parserState.PopBlocksByKind("change")
	for _, block := range changeBlocks {
		h, parsedOk := ParseChangeBlock(block)
		if !parsedOk {
			return newParserState, fmt.Errorf("unparseable change block with boundary %s", block.Boundary)
		}
		if err := ApplyChangeBlockStore(store, h); err != nil {
			return newParserState, fmt.Errorf("apply change block %s %s: %w", h.Op, h.Target, err)
		}
	}
	return newParserState, nil
}
