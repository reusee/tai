package codes

import (
	"github.com/reusee/tai/codes/codetypes"
)

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

// FinishBlockSystemPrompt teaches the model the finish block format.
// It is separate from the boundary diff handler because the finish block is
// a generic block kind, not specific to diff handling. See TheoryOfFinishBlock.
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
`

// FinishBlockRestatePrompt provides the finish block instructions for the
// restate prompt. It is separate from the boundary diff handler because the
// finish block is a generic block kind. See TheoryOfFinishBlock.
const FinishBlockRestatePrompt = `- After all change blocks, generate a finish block with a one-sentence summary of all changes made:
:::finish <random_boundary>
<one-sentence summary>
:::end <random_boundary>
- The finish block MUST be the last block in the response.
- If no changes were made, generate a finish block with "No changes were needed." as the summary.
`

// RestatePrompt assembles the full restate prompt from the diff handler
// and the finish block restate. It is separate from the DiffHandler interface
// because the finish block is not part of the diff handler. See TheoryOfFinishBlock.
type RestatePrompt string

func (Module) RestatePrompt(
	diffHandler codetypes.DiffHandler,
) RestatePrompt {
	return RestatePrompt(
		diffHandler.RestatePrompt() + "\n" +
			FinishBlockRestatePrompt,
	)
}