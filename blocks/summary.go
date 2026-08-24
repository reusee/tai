package blocks

const TheoryOfSummaryBlocks = `
Summary blocks serve two purposes: they provide a brief description of each
generation round's content (including reasoning and actions taken) and act as the
completion signal that tells the system a round has ended normally. One summary
block is emitted per round, after all other blocks except any continue block; when
a continue block is present, the summary block appears before it. The body is a
markdown bullet list using the "-" format; each item is a single short, concise
phrase so the user can quickly scan what was done and thought without reading dense
paragraphs or long sentences. Summaries are collected after generation ends and
displayed alongside the round statistics, providing a human-readable narrative of
the session without interfering with block processing or state management. Summary
blocks are always enabled because they have no side effects and help the user
understand what the model did and thought in each round without reading the full
output.

No block kind replaces or implies the summary: a round that ends without a
summary block is retried, including rounds carrying component-triggering blocks
(see pipeline.TheoryOfLoops), and the retry feedback names the missing summary.
Every kind whose prompt stops and waits for the next round (shell, go-test,
go-src, read) phrases its stop rule as "end the response with a summary block",
declares that its block does not replace the summary, and adds the sequence rule
that the block after its closing line must be the summary block; the summary
prompts add a closing self-check so the model verifies its last block before
ending the response. When the retry budget is exhausted, the loop synthesizes a
summary so the round statistics and the summary display keep the round's
narrative. When no changes were made, the summary block body should be "No
changes were needed." so the model still signals normal completion.
`

const SummaryBlockSystemPrompt = `
**Summary Block Kind:**

Use the "summary" kind to provide a brief description of the current generation round's content, including the reasoning and actions taken, and to signal that the round is complete. Emit exactly one summary block at the end of each generation round.

**Rules:**
- Emit exactly one summary block per generation round.
- The summary block MUST appear after all other blocks except continue blocks. When a continue block is present, the summary block MUST appear before it, and the continue block is the last block in the response.
- The body MUST contain ONLY the markdown bullet list in the "- " format; each item is a single short, concise phrase describing what was done or thought in this round. No prose and no other text inside the block.
- Keep each list item brief and easy to scan. Do not write long sentences or dense paragraphs.
- The summary is displayed to the user after generation ends, alongside round statistics.
- A summary block is required in EVERY response, even when no change blocks are emitted. When no changes were made, use "No changes were needed." as the only bullet point. Omitting the summary block is a rule violation: the system discards the entire response and retries it, so none of its blocks take effect.
- **Closing self-check (run it every time)**: before ending a response, look at the last block you emitted. If it is anything other than a summary block — or a continue block that follows a summary block — the response is incomplete: emit the summary block now. No other block kind can close a response; a response that ends without a summary block is discarded and retried, so none of its blocks take effect.
`

const SummaryBlockRestatePrompt = `- After all other blocks, generate a summary block whose body is a bullet list of what was done.
- The summary block MUST appear after all other blocks. When a continue block is present, the summary block comes before it, and the continue block is the last block.
- A summary block is required in every response, even when no change blocks are emitted. If no changes were made, generate a summary block with "No changes were needed." as the only bullet point.
- Closing self-check: before ending the response, check the last block you emitted. If it is not a summary block (and not a continue block following one), emit the summary block now. A response ending on any other block is discarded and retried; none of its blocks take effect.`
