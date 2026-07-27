package blocks

const TheoryOfSummaryBlocks = `
Summary blocks allow the model to emit a brief description of each generation
round's content, including reasoning. One summary block is emitted per round,
before any continue or finish block. The body is a markdown bullet list using
the "-" format; each item is a single short, concise phrase so the user can
quickly scan what was done and thought without reading dense paragraphs or
long sentences. The summaries are collected after generation ends and displayed
alongside the round statistics, providing a human-readable narrative of the
generation session without interfering with block processing or state
management. Summary blocks are always enabled because they have no side effects
and provide value in every session: they help the user understand what the
model did and thought in each round without reading the full output.
`

const SummaryBlockSystemPrompt = `**Summary Block Kind:**

The "summary" kind provides a brief description of the current generation round's content, including your reasoning and actions taken. One summary block MUST be emitted at the end of each generation round, before any continue or finish block.

**Summary Block Format:**

:::<boundary> <summary>
- <short point 1>
- <short point 2>
:::<boundary> </summary>

**Rules:**
- Emit exactly one summary block per generation round.
- The summary block MUST appear before any continue or finish block in the response.
- The body MUST be a markdown bullet list using the "-" format. Each item is a single short, concise phrase describing what was done or thought in this round.
- Keep each list item brief and easy to scan. Do not write long sentences or dense paragraphs.
- The summary is displayed to the user after generation ends, alongside round statistics.
- The boundary is a random string chosen by the AI to prevent conflicts with the body content.

**Example:**

:::<boundary> <summary>
- Identified root cause in the parser
- Added boundary-matching fix
- Updated tests for unclosed blocks
:::<boundary> </summary>
`

// ProcessSummaryBlocks processes all summary blocks and returns their body
// texts. Summaries are collected for terminal display after generation ends,
// not appended to the state. See TheoryOfSummaryBlocks.
func ProcessSummaryBlocks(blocks []Block) []string {
	if len(blocks) == 0 {
		return nil
	}
	var summaries []string
	for _, block := range blocks {
		summaries = append(summaries, block.Body)
	}
	return summaries
}
