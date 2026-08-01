package blocks

import (
	"github.com/reusee/tai/generators"
)

const TheoryOfContinueBlocks = `
Continue blocks are a generic self-prompting mechanism with no prescribed
semantics. When a response ends with a continue block, the system extracts the
block body, feeds it back verbatim as the next user message, and automatically
starts a new generation round. By chaining rounds this way, the model can
produce arbitrarily long outputs without hitting the single-request generation
limit. Summary blocks and continue blocks can coexist in the same response:
the summary block marks the round as complete, and the continue block prompts
the next round's input. The two are not mutually exclusive. When both are
present, the summary block must appear before the continue block, and the
continue block must be the last block in the response.

The mechanism is orthogonal to the conventions layered on top of it. The body
is opaque to the mechanism: it carries no meaning beyond being fed back as
user input. Task lists, planning rounds, and decomposition strategies (see
TheoryOfMandatoryPlanning and TheoryOfTaskDecomposition) are extensions that
use continue blocks as their transport; they define what the body contains,
but they do not define the mechanism. Future extensions may use continue
blocks for entirely different purposes, so this definition must remain free of
any single extension's semantics.
`

const ContinueBlockSystemPrompt = `
Continue Block Kind:

The "continue" kind signals that the current generation round is over and another round should follow. The block body is fed back verbatim as the next user message, providing self-supplied input for the next round. It MUST be the last block in the response, after all other blocks including the summary block.

The continue block is a generic self-prompting mechanism with no prescribed content. Conventions layered on top of it (for example, the mandatory planning mandate, which carries the evolving task list in the body) define what the body should contain; the mechanism itself imposes none.

**Continue Block Format (complete example):**

<<灪麤 <continue>
Continue the task: apply the remaining changes and verify them with tests.
灪麤

The delimiter 灪麤 in the example is illustrative only: in every block emitted, choose exactly two uncommon Chinese characters as the delimiter, and use the same delimiter on the closing line. The opening marker must start at the beginning of a line, and the closing line is the delimiter alone on its own line. Never write the placeholder text "DELIMITER" or reuse an example delimiter in a real marker.

**Rules:**
- The body is fed back verbatim as the next user message and triggers a new generation round.
- Use a continue block whenever another generation round is needed — for example, when the remaining output would exceed a single response's capacity.
- A response may end with both a summary block and a continue block: the summary block marks the round as complete, and the continue block prompts the next round's input. They are not mutually exclusive.
- The continue block MUST be the last block in the response, after the summary block; no other blocks may appear after it.
`

const ContinueBlockRestatePrompt = `- Continue block: when another generation round is needed, emit:
<<龖爨 <continue>
<next user message content>
龖爨
It MUST be the last block in the response. The body is fed back verbatim as the next user message to trigger a new round. The example delimiter 龖爨 is illustrative: choose two uncommon Chinese characters as the delimiter, the SAME delimiter on the closing line. The opening marker starts at the beginning of a line; the closing line is the delimiter alone. Never write the placeholder text "DELIMITER" or reuse an example delimiter literally.`

// ProcessContinueBlocks processes all continue blocks and returns their body
// texts as generator parts. Each block's body becomes a Text part that will be
// fed back as the next user message to trigger a new generation round.
// See TheoryOfContinueBlocks.
func ProcessContinueBlocks(blocks []Block) []generators.Part {
	if len(blocks) == 0 {
		return nil
	}
	var parts []generators.Part
	for _, block := range blocks {
		parts = append(parts, generators.Text(block.Body))
	}
	return parts
}
