package changes

import (
	"fmt"

	"github.com/reusee/tai/blocks"
)

// ChangeBlockHandler is a BlockHandler for change blocks: it parses each
// change block and applies it to the given FileStore. It returns
// consumed=true for change blocks (whether or not application succeeds, so
// failed blocks are not re-processed by components) and consumed=false for
// all other block kinds. Application failures return an ApplyError so the
// generation loop can provide change-block-specific retry guidance.
// See loops.TheoryOfLoops.
type ChangeBlockHandler func(block blocks.Block) (bool, error)

// BuildChangeBlockHandler returns a factory that creates a ChangeBlockHandler
// for the given FileStore. Callers (codes, next) construct the handler with
// their in-memory or on-disk store and share the change-application logic,
// including ApplyError construction for parse and apply failures.
// ApplyChangeBlockStore is captured from the dscope scope.
// See TheoryOfDscopeProvidedApplyFunctions.
type BuildChangeBlockHandler func(store FileStore) ChangeBlockHandler

func (Module) BuildChangeBlockHandler(
	applyChangeBlockStore ApplyChangeBlockStore,
) BuildChangeBlockHandler {
	return func(store FileStore) ChangeBlockHandler {
		return func(block blocks.Block) (bool, error) {
			if block.Kind != "change" {
				return false, nil
			}
			h, parsedOk := ParseChangeBlock(block)
			if !parsedOk {
				return false, &ApplyError{
					Err: fmt.Errorf("unparseable change block with boundary %s", block.Boundary),
				}
			}
			if err := applyChangeBlockStore(store, h); err != nil {
				return false, &ApplyError{
					Err: fmt.Errorf("apply change block %s %s: %w", h.Op, h.Target, err),
				}
			}
			return true, nil
		}
	}
}
