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

const TheoryOfBatchDiffWrite = `
The diff file is mutated in memory as change blocks are applied and persisted only once
at the end of processing (or on early exit), rather than after every hunk. This reduces
I/O from O(N*S) to O(S) for N hunks in a file of size S, without changing the on-disk
result: applied change blocks are removed and non-change blocks (e.g., finish summaries)
are preserved exactly as before.
`

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
		content, err := root.ReadFile(diffFilePath)
		if err != nil {
			// Absolute paths (e.g., /tmp/...) are rejected by os.Root
			// because they escape the root's relative namespace. Fall
			// back to os.ReadFile so diff files at absolute paths remain
			// accessible. See test cases using t.TempDir().
			content, err = os.ReadFile(diffFilePath)
			if err != nil {
				yield(codetypes.Hunk{}, err)
				return
			}
		}

		// writeDiff persists the current in-memory content to the diff file.
		// Called once at the end of processing instead of after every hunk,
		// reducing I/O from O(N*S) to O(S) for N hunks in a file of size S.
		// See TheoryOfBatchDiffWrite.
		writeDiff := func() error {
			trimmed := bytes.TrimSpace(content)
			if err := root.WriteFile(diffFilePath, trimmed, 0644); err != nil {
				return os.WriteFile(diffFilePath, trimmed, 0644)
			}
			return nil
		}

		modified := false
		cursor := 0
		for {
			block, relStart, relEnd, ok, err := blocks.ParseFirstBlock(content[cursor:])
			if err != nil {
				if modified {
					writeDiff()
				}
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
			h, parsedOk := blocks.ParseChangeBlock(block)
			if !parsedOk {
				// Unparseable change blocks are not applied and therefore
				// preserved rather than deleted from the diff file.
				cursor = end
				continue
			}
			if err := applyHunk(root, h); err != nil {
				if modified {
					writeDiff()
				}
				yield(h, fmt.Errorf("hunk %s %s: %w", h.Op, h.Target, err))
				return
			}
			// Remove the applied change block from in-memory content; the
			// disk write is deferred to the end of processing.
			content = append(content[:start], content[end:]...)
			modified = true
			cursor = min(start, len(content))
			if !yield(h, nil) {
				writeDiff()
				return
			}
		}
		if modified {
			if err := writeDiff(); err != nil {
				yield(codetypes.Hunk{}, err)
				return
			}
		}
	}
}
