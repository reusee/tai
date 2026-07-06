package blocks

const TheoryOfFinishBlock = `
The finish block is a terminal signal placed at the end of the AI's output. It
contains a one-sentence summary of all changes made. The Apply method skips
non-change blocks (including finish) without error, treating them as informational
metadata rather than file modifications. Only successfully applied change blocks
are removed from the diff file; non-change blocks and unparseable change blocks
are preserved so the summary and any unprocessed content remain available after
processing. This provides a clear completion marker and a human-readable summary
without interfering with hunk processing.
`

const FinishBlockSystemPrompt = `**Finish Block Kind:**

The "finish" kind signals the end of all code modifications and provides a one-sentence summary of the changes made. It MUST be the last block in the response, after all change blocks.

**Finish Block Format:**

:::finish <boundary>
<one-sentence summary of all changes>
:::end <boundary>

**Rules:**
- The finish block MUST be the last block in the response, after all change blocks.
- The body is a single sentence summarizing what was done.
- Use the same boundary format (two random uncommon meaningless Chinese characters) as change blocks.
- Generate exactly one finish block per response.
- The closing :::end marker MUST use the same boundary as the opening :::finish marker. A line-start :::end with a different boundary is treated as body content, not a closing marker, so the block remains unclosed.
- **Avoid body-content characters**: Select boundary characters that do not appear anywhere in the summary sentence. If the summary contains Chinese text, pick boundary ideographs absent from that text so a body line starting with ":::end " cannot accidentally match the boundary and prematurely close the block.
`

const FinishBlockRestatePrompt = `- After all change blocks, generate a finish block with a one-sentence summary of all changes made:
:::finish <random_boundary>
<one-sentence summary>
:::end <random_boundary>
- The finish block MUST be the last block in the response.
- If no changes were made, generate a finish block with "No changes were needed." as the summary.
- The closing :::end marker MUST use the same boundary as the opening :::finish marker; a line-start :::end with a different boundary is treated as body content, not a closing marker.
- **Avoid body-content characters**: Pick boundary characters that do not appear in the summary sentence, especially if the summary contains Chinese text, so a body line starting with ":::end " cannot accidentally match the boundary and prematurely close the block.
`