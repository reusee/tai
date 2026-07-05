package codes

import (
	"bytes"
	"fmt"
	"iter"
	"os"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
)

// BoundaryDiffHandler implements the DiffHandler interface using a boundary-delimited format.
// Changes are wrapped in :::change <boundary> / :::end <boundary> blocks, where the boundary
// is a random string chosen by the AI to prevent parsing conflicts with code content.
// This format eliminates escape requirements (unlike XML) while maintaining structural parseability.
type BoundaryDiffHandler struct{}

var _ codetypes.DiffHandler = BoundaryDiffHandler{}

func (b BoundaryDiffHandler) Functions() []*generators.Function {
	return nil
}

func (b BoundaryDiffHandler) SystemPrompt() string {
	return blocks.BlockFormatSystemPrompt + "\n" + blocks.ChangeBlockSystemPrompt
}

func (b BoundaryDiffHandler) RestatePrompt() string {
	return blocks.ChangeBlockRestatePrompt
}

func (b BoundaryDiffHandler) Apply(root *os.Root, diffFilePath string) iter.Seq2[codetypes.Hunk, error] {
	return func(yield func(codetypes.Hunk, error) bool) {
		content, err := os.ReadFile(diffFilePath)
		if err != nil {
			yield(codetypes.Hunk{}, err)
			return
		}
		cursor := 0
		for {
			block, relStart, relEnd, ok, err := blocks.ParseFirstBlock(content[cursor:])
			if err != nil {
				yield(codetypes.Hunk{}, err)
				return
			}
			if !ok {
				break
			}
			start := cursor + relStart
			end := cursor + relEnd
			// Non-change blocks (e.g., finish summary) carry no file
			// modifications and are preserved in the diff file. Only
			// successfully applied change blocks are removed. See
			// TheoryOfFinishBlock.
			if block.Kind != "change" {
				cursor = end
				continue
			}
			h, parsedOk := blocks.ParseChangeXMLBody(block.Body)
			if !parsedOk {
				// Unparseable change blocks are not applied and therefore
				// preserved rather than deleted from the diff file.
				cursor = end
				continue
			}
			if err := applyHunk(root, h); err != nil {
				yield(h, fmt.Errorf("hunk %s %s: %w", h.Op, h.Target, err))
				return
			}
			// Remove only the applied change block from the diff file. The
			// in-memory content is kept untrimmed so block offsets stay
			// stable for subsequent searches; the persisted file is trimmed
			// for cleanliness.
			newContent := append(content[:start], content[end:]...)
			if err := os.WriteFile(diffFilePath, bytes.TrimSpace(newContent), 0644); err != nil {
				yield(codetypes.Hunk{}, err)
				return
			}
			content = newContent
			// Everything before `start` has already been processed (preserved
			// non-change blocks or previously removed change blocks), so
			// resume searching from `start`, clamped to the content length.
			cursor = min(start, len(content))
			if !yield(h, nil) {
				return
			}
		}
	}
}
