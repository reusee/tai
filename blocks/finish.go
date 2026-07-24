package blocks

const TheoryOfFinishBlock = `
The finish block is a terminal signal placed at the end of the AI's output. It
contains a one-sentence summary of the entire response content, encompassing
analysis, context requests, code changes, test runs, and any other actions taken
during the round — not merely the change blocks. The Apply method skips
non-change blocks (including finish) without error, treating them as informational
metadata rather than file modifications. Only successfully applied change blocks
are removed from the diff file; non-change blocks and unparseable change blocks
are preserved so the summary and any unprocessed content remain available after
processing. This provides a clear completion marker and a human-readable summary
without interfering with hunk processing.

The finish block is mandatory in every response, even when no change blocks are
emitted. The generation loop uses the presence of a completion block (summary or
finish) to distinguish a normally ended round from truncated output; omitting the
finish block causes the system to assume the output was incomplete and retry the
round unnecessarily. When no changes were made, the finish block body is
"No changes were needed." so the model still signals normal completion.
`

const FinishBlockSystemPrompt = `**Finish Block Kind:**

The "finish" kind signals the end of all code modifications and provides a one-sentence summary of the entire response content. It MUST be the last block in the response, after all change blocks.

**Finish Block Format:**

:::<boundary> <finish>
<one-sentence summary of the entire response content>
:::<boundary> </finish>

**Rules:**
- The finish block MUST be the last block in the response, after all change blocks.
- The body is a single sentence summarizing the entire response content, including analysis, context requests, code changes, test runs, and any other actions taken — not just the change blocks.
- Generate exactly one finish block per response.
- A finish block is required in EVERY response, even when no change blocks are emitted. When no changes were made, use "No changes were needed." as the summary body. Omitting the finish block causes the system to treat the output as truncated and retry the round unnecessarily.
`

const FinishBlockRestatePrompt = `- After all change blocks, generate a finish block with a one-sentence summary of the entire response content:
:::<boundary> <finish>
<one-sentence summary>
:::<boundary> </finish>
- The finish block MUST be the last block in the response.
- The summary covers the entire response content, not just the change blocks.
- A finish block is required in every response, even when no change blocks are emitted. If no changes were made, generate a finish block with "No changes were needed." as the summary.
`
