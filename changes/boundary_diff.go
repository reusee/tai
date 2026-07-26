package changes

import (
	"bytes"
	"fmt"
	"iter"
	"os"

	"github.com/reusee/tai/blocks"
)

const TheoryOfBatchDiffWrite = `
The diff file is mutated in memory as change blocks are applied and persisted only once
at the end of processing (or on early exit), rather than after every change block. This
reduces I/O from O(N*S) to O(S) for N change blocks in a file of size S, without changing
the on-disk result: applied change blocks are removed and non-change blocks (e.g.,
finish summaries) are preserved exactly as before.
`

// ChangeBlockSystemPrompt returns the system prompt describing the change
// block format, composed of the shared block format prompt and the
// change-specific prompt. It is used by the change block component and the
// "next" subcommand to teach the model how to emit change blocks.
func ChangeBlockSystemPrompt() string {
	return blocks.BlockFormatSystemPrompt + "\n" + ChangeBlockPrompt
}

// ChangeBlockRestatePrompt returns the short critical reminder that reinforces
// the change block format rules. It is used by the change block component as
// its RestatePrompt field.
func ChangeBlockRestatePrompt() string {
	return ChangeBlockRestatePromptText
}

// ApplyDiffFile streams change blocks from a boundary-delimited diff file,
// applies each one to the working tree rooted at root, and removes
// successfully applied change blocks from the diff file (persisted once at
// the end of processing). Non-change blocks (e.g., finish summaries) are
// preserved. It yields each applied ChangeBlock, or an error that aborts
// processing. See TheoryOfBatchDiffWrite.
func ApplyDiffFile(root *os.Root, diffFilePath string) iter.Seq2[ChangeBlock, error] {
	return func(yield func(ChangeBlock, error) bool) {
		content, err := root.ReadFile(diffFilePath)
		if err != nil {
			// Absolute paths (e.g., /tmp/...) are rejected by os.Root
			// because they escape the root's relative namespace. Fall
			// back to os.ReadFile so diff files at absolute paths remain
			// accessible. See test cases using t.TempDir().
			content, err = os.ReadFile(diffFilePath)
			if err != nil {
				yield(ChangeBlock{}, err)
				return
			}
		}

		// writeDiff persists the current in-memory content to the diff
		// file. Called once at the end of processing instead of after
		// every change block, reducing I/O from O(N*S) to O(S) for N
		// change blocks in a file of size S. See TheoryOfBatchDiffWrite.
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
				yield(ChangeBlock{}, err)
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
			h, parsedOk := ParseChangeBlock(block)
			if !parsedOk {
				// Unparseable change blocks are not applied and
				// therefore preserved rather than deleted from the
				// diff file.
				cursor = end
				continue
			}
			if err := ApplyChangeBlock(root, h); err != nil {
				if modified {
					writeDiff()
				}
				yield(h, fmt.Errorf("change block %s %s: %w", h.Op, h.Target, err))
				return
			}
			// Remove the applied change block from in-memory content;
			// the disk write is deferred to the end of processing.
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
				yield(ChangeBlock{}, err)
				return
			}
		}
	}
}
