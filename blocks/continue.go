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

The "continue" kind signals that the current generation round is over and another round should follow. The block body is fed back to you verbatim as the next user message, letting you supply yourself with input for the next round. It MUST be the last block in the response, after all other blocks including the summary block.

The continue block is a generic self-prompting mechanism with no prescribed content. Conventions layered on top of it (for example, the mandatory planning mandate, which carries the evolving task list in the body) define what the body should contain; the mechanism itself imposes none.

**Continue Block Format (complete example):**

:::鸑鷟 <continue>
Continue the task: apply the remaining changes and verify them with tests.
:::鸑鷟 </continue>

The boundary 鸑鷟 in the example is illustrative only: in every block you emit, use a freshly chosen pair of two uncommon, meaningless Chinese characters, and repeat the exact same pair in the closing marker. Every marker line starts at the beginning of a line and ends with the '>' of its tag. Never write the placeholder text "<boundary>" in a real marker.

**Rules:**
- The body is fed back verbatim as the next user message and triggers a new generation round.
- Use a continue block whenever another generation round is needed — for example, when the remaining output would exceed a single response's capacity.
- A response may end with both a summary block and a continue block: the summary block marks the round as complete, and the continue block prompts the next round's input. They are not mutually exclusive.
- The continue block MUST be the last block in the response, after the summary block; no other blocks may appear after it.
`

const ContinueBlockRestatePrompt = `- Continue block: when another generation round is needed, emit:
:::鸑鷟 <continue>
<next user message content>
:::鸑鷟 </continue>
It MUST be the last block in the response. The body is fed back verbatim as the next user message to trigger a new round. The example boundary 鸑鷟 is illustrative: use your own fresh pair of TWO Chinese characters, the SAME pair in both markers, each marker on its own line ending with '>'. Never write the placeholder text "<boundary>" literally.`

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
