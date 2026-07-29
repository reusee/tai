package blocks

const TheoryOfSummaryBlocks = `
Summary blocks serve a dual purpose: they provide a brief description of each
generation round's content (including reasoning and actions taken) and act as
the completion signal that tells the system a round has ended normally. One
summary block is emitted per round, after all other blocks except any continue
block. When a continue block is present, the summary block appears before it.
The body is a markdown
bullet list using the "-" format; each item is a single short, concise phrase
so the user can quickly scan what was done and thought without reading dense
paragraphs or long sentences. The summaries are collected after generation
ends and displayed alongside the round statistics, providing a human-readable
narrative of the generation session without interfering with block processing
or state management. Summary blocks are always enabled because they have no
side effects and provide value in every session: they help the user understand
what the model did and thought in each round without reading the full output.
The summary block also serves as the round completion signal: when a round
ends without a summary block, the system assumes the output was truncated and
retries the round. When no changes were made, the summary block body should be
"No changes were needed." so the model still signals normal completion.
`

const SummaryBlockSystemPrompt = `**Summary Block Kind:**

The "summary" kind provides a brief description of the current generation round's content, including your reasoning and actions taken, and signals that the round is complete. One summary block MUST be emitted at the end of each generation round.

**Summary Block Format:**

:::<boundary> <summary>
- <short point 1>
- <short point 2>
:::<boundary> </summary>

**Rules:**
- Emit exactly one summary block per generation round.
- The summary block MUST appear after all other blocks except continue blocks. When a continue block is present, the summary block MUST appear before it, and the continue block is the last block in the response.
- The body MUST be a markdown bullet list using the "-" format. Each item is a single short, concise phrase describing what was done or thought in this round.
- Keep each list item brief and easy to scan. Do not write long sentences or dense paragraphs.
- The summary is displayed to the user after generation ends, alongside round statistics.
- A summary block is required in EVERY response, even when no change blocks are emitted. When no changes were made, use "No changes were needed." as the only bullet point. Omitting the summary block causes the system to treat the output as truncated and retry the round unnecessarily.
- The boundary is a random string chosen by the AI to prevent conflicts with the body content.

**Example:**

:::<boundary> <summary>
- Identified root cause in the parser
- Added boundary-matching fix
- Updated tests for unclosed blocks
:::<boundary> </summary>
`

const SummaryBlockRestatePrompt = `- After all other blocks, generate a summary block with a bullet list of what was done:
:::<boundary> <summary>
- short point 1
- short point 2
:::<boundary> </summary>
- The summary block MUST appear after all other blocks. When a continue block is present, the summary block comes before it, and the continue block is the last block.
- A summary block is required in every response, even when no change blocks are emitted. If no changes were made, generate a summary block with "No changes were needed." as the only bullet point.`

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
