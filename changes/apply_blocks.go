package changes

import (
	"fmt"
	"os"

	"github.com/reusee/tai/blocks"
)

// ApplyChangeBlocks applies all change blocks to the working tree via
// ApplyChangeBlock. It returns an error if any block is unparseable or if
// ApplyChangeBlock fails. See TheoryOfImmediateApply.
func ApplyChangeBlocks(blocks []blocks.Block, root *os.Root) error {
	return ApplyChangeBlocksStore(blocks, NewRootStore(root))
}

// ApplyChangeBlocksStore applies all change blocks to the given FileStore via
// ApplyChangeBlockStore. It returns an error if any block is unparseable or
// if ApplyChangeBlockStore fails. When the store is a MemoryStore, changes
// are buffered in memory and only written to disk on Flush.
// See TheoryOfInMemoryApply.
func ApplyChangeBlocksStore(blocks []blocks.Block, store FileStore) error {
	for _, block := range blocks {
		h, parsedOk := ParseChangeBlock(block)
		if !parsedOk {
			return fmt.Errorf("unparseable change block with boundary %s", block.Boundary)
		}
		if err := ApplyChangeBlockStore(store, h); err != nil {
			return fmt.Errorf("apply change block %s %s: %w", h.Op, h.Target, err)
		}
	}
	return nil
}
