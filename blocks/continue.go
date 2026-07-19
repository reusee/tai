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
limit. Each round must end with either a finish block (session complete) or a
continue block (another round follows), but not both, and the continue block
must be the last block in the response.

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

The "continue" kind signals that the current generation round is over and another round should follow. The block body is fed back to you verbatim as the next user message, letting you supply yourself with input for the next round. It MUST be the last block in the response, after all other blocks.

The continue block is a generic self-prompting mechanism with no prescribed content. Conventions layered on top of it (for example, the mandatory planning mandate, which carries the evolving task list in the body) define what the body should contain; the mechanism itself imposes none.

**Continue Block Format:**

:::<boundary> <continue>
<next user message content>
:::<boundary> </continue>

**Rules:**
- The body is fed back verbatim as the next user message and triggers a new generation round.
- Use a continue block whenever another generation round is needed — for example, when the remaining output would exceed a single response's capacity.
- A response MUST contain either a finish block (if the session is complete) or a continue block (if another round is needed), but NOT both.
- The continue block MUST be the last block in the response; no other blocks may appear after it.
- The boundary is a random string chosen by the AI to prevent conflicts with the body content.
`

// ProcessContinueBlocks pops all continue blocks from parserState and returns
// their body texts as generator parts for appending as user content.
// It does not append to the state directly; callers are responsible for
// building the user content and appending it.
func ProcessContinueBlocks(parserState *ParserState) []generators.Part {
	if parserState == nil {
		return nil
	}
	blocks := parserState.PopBlocksByKind("continue")
	if len(blocks) == 0 {
		return nil
	}
	var parts []generators.Part
	for _, block := range blocks {
		parts = append(parts, generators.Text(block.Body))
	}
	return parts
}
