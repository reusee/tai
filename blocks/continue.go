package blocks

import (
	"github.com/reusee/tai/generators"
)

const TheoryOfContinueBlocks = `
Continue blocks are a generic self-prompting mechanism: the system extracts
the block body of a response-ending continue block, feeds it back verbatim
as the next user message, and starts a new generation round. Chained rounds
let the model produce arbitrarily long outputs without hitting the
single-request generation limit. ContinueBlockSystemPrompt is itself the
theory text for the emission and ordering rules — the summary coexistence,
the continue-last position, and the incomplete-retry consequence — and they
are not repeated here; the loop-level enforcement lives in
pipeline.TheoryOfLoops.

The mechanism is orthogonal to the conventions layered on top of it: the body
is opaque to the mechanism, carrying no meaning beyond being fed back as user
input. Task lists, planning rounds, and decomposition strategies are
extensions that use continue blocks as transport; they define what the body
contains, not the mechanism.
`

const ContinueBlockSystemPrompt = `
Continue Block Kind:

Use the "continue" kind to signal that the current generation round is over and another round should follow. The block body is fed back verbatim as the next user message, providing self-supplied input for the next round. Place it as the last block in the response, after all other blocks including the summary block.

The continue block is a generic self-prompting mechanism with no prescribed content. Conventions layered on top of it (for example, the mandatory planning mandate, which carries the evolving task list in the body) define what the body should contain; the mechanism itself imposes none.

**Rules:**
- The body is fed back verbatim as the next user message and triggers a new generation round.
- Use a continue block whenever another generation round is needed — for example, when the remaining output would exceed a single response's capacity.
- A response may end with both a summary block and a continue block: the summary block marks the round as complete, and the continue block prompts the next round's input. They are not mutually exclusive.
- The continue block MUST be the last block in the response, after the summary block; no other blocks may appear after it.
- A continue block does NOT replace the summary block. A response carrying a continue block still requires a summary block before it: emit the summary block first, then the continue block. A response that ends without a summary block — including one that ends with only a continue block — is treated as incomplete and retried: its blocks are discarded and must be re-emitted.
`

// ProcessContinueBlocks processes all continue blocks and returns their body
// texts as generator parts. Only blocks with Kind "continue" are processed.
// Each block's body becomes a Text part that will be fed back as the next
// user message to trigger a new generation round. See TheoryOfContinueBlocks.
func ProcessContinueBlocks(blocks []Block) []generators.Part {
	if len(blocks) == 0 {
		return nil
	}
	var parts []generators.Part
	for _, block := range blocks {
		if block.Kind != "continue" {
			continue
		}
		parts = append(parts, generators.Text(block.Body))
	}
	return parts
}
