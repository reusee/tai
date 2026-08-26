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
go-src, read) phrases its stop rule summary-first — emit the summary block
immediately after the kind's closing line, then end the response — and forbids
stopping at the closing line itself; no stop-and-wait prompt places a bare stop
instruction before the summary requirement, because a model that reads "stop"
first halts at the closing line and omits the summary, the observed failure
shape of a lone go-src block ending a response. Each prompt declares that its
block does not replace the summary and adds the sequence rule that the block
after its closing line must be the summary block; the summary prompt adds a
closing self-check so the model verifies its last block before ending the
response, and it explicitly covers the fetch-only response shape — a response
whose only blocks are read, go-src, shell, or go-test blocks still requires the
summary, because the system reads the summary as its only proof that the
response was generated completely and followed the rules. When the retry budget
is exhausted, the loop synthesizes a summary so the round statistics and the
summary display keep the round's narrative. When no changes were made, the
summary block body should be "No changes were needed." so the model still
signals normal completion.
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
- A summary block is required in EVERY response, even when no change blocks are emitted — including a response whose only blocks are read, go-src, shell, or go-test blocks. When no changes were made, use "No changes were needed." as the only bullet point.
- The summary block is the completion signal the system uses to verify that the response was generated completely and followed the rules; without it the system cannot confirm the round ended normally. It is never omittable, never deferrable to a later response, and never replaceable by any other block. Omitting it is a rule violation: the system discards the entire response and retries it, so none of its blocks take effect.
- **Closing self-check (run it every time)**: before ending a response, look at the last block you emitted. If it is anything other than a summary block — or a continue block that follows a summary block — the response is incomplete: emit the summary block now. No other block kind can close a response; a response that ends without a summary block is discarded and retried, so none of its blocks take effect.
`
