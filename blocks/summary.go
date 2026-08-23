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
output. The summary requirement is shared by every kind prompt: each kind whose
prompt stops and waits for the next round (shell, go-test, go-src, read)
phrases its stop rule as "end the response with a summary block" and declares that
its block does not replace the summary, so no stop instruction conflicts with the
every-response requirement. A round with no summary block and no component-
triggering block is assumed truncated and retried; a round carrying a component-
triggering block is complete without a summary (see loops.TheoryOfLoops), which is
why every kind prompt still demands one — the round statistics and the summary
display would otherwise lose the round's narrative. When no changes were made, the
summary block body should be "No changes were needed." so the model still signals
normal completion.
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
- A summary block is required in EVERY response, even when no change blocks are emitted. When no changes were made, use "No changes were needed." as the only bullet point. Omitting the summary block causes the system to treat the output as truncated and retry the round unnecessarily.
`

const SummaryBlockRestatePrompt = `- After all other blocks, generate a summary block whose body is a bullet list of what was done.
- The summary block MUST appear after all other blocks. When a continue block is present, the summary block comes before it, and the continue block is the last block.
- A summary block is required in every response, even when no change blocks are emitted. If no changes were made, generate a summary block with "No changes were needed." as the only bullet point.`
