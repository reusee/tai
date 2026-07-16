package blocks

import (
	"github.com/reusee/tai/generators"
)

const TheoryOfContinueBlocks = `
Continue blocks allow the model to self-drive multi-turn generation by emitting
a continue block at the end of a response when the task is not yet complete.
The system parses the continue block, extracts its body as the next user message,
and automatically starts a new generation round. This enables the model to
produce arbitrarily long outputs by chaining multiple rounds. Each round must
end with either a finish block (task complete) or a continue block (more work
needed), but not both.

The primary trigger for using continue blocks is the number of expected change
blocks: when a task requires more than approximately 5-7 change blocks, the
model should decompose it into multiple rounds. Secondary triggers include
natural phase boundaries (e.g., interface refactoring followed by caller
updates) and dependency chains where later steps depend on earlier results.
Each round should produce a coherent, reviewable set of changes; prefer fewer,
larger rounds over many tiny rounds to minimize round-trip overhead.

For complex tasks, the model maintains a task list in the continue block body.
In each round, the model selects one or more tasks from the list to execute,
produces the corresponding change blocks, and ends with a continue block
containing the updated task list — marking completed tasks and listing
remaining tasks. This cycle repeats until all tasks are complete, at which
point a finish block is used instead. This avoids hitting the single-request
generation limit and keeps each round focused and reviewable.
Simple tasks that fit within a single response need not be decomposed.
`

const ContinueBlockSystemPrompt = `
Continue Block Kind:

The "continue" kind signals that the task is not yet complete and more rounds of generation are needed. It MUST be the last block in the response, after all change blocks.

**When to Use Continue Blocks:**

Use a continue block when any of the following conditions apply:
- The task requires more than approximately 5-7 change blocks, which may exceed a single response's practical output capacity. Counting the expected change blocks before starting is the most reliable trigger.
- The task naturally decomposes into independent phases with clear boundaries (e.g., "refactor the interface" followed by "update all callers"). Each phase becomes a separate round.
- Later steps depend on the results or review of earlier steps, making incremental delivery safer than producing all changes at once.
- The estimated total output (code bodies plus explanatory prose) would approach or exceed the model's per-response limit, risking truncation.

Do NOT use continue blocks for:
- Simple, atomic changes that fit comfortably in one response (typically ≤5 change blocks).
- Single-file modifications with few change blocks.
- Tasks where all changes are tightly coupled and reviewing them together is essential for correctness.

**Round Granularity:**
Each round should produce a coherent, reviewable set of changes. Prefer fewer, larger rounds over many tiny rounds to reduce round-trip overhead. A round that produces only one trivial change block wastes the continue mechanism; group related changes into the same round.

**Task Decomposition Strategy:**
When a task warrants continue blocks, first conceive the overall process, break it down into specific subtasks, and generate a concrete task list. The continue block body contains this task list. In each round, select one or more tasks from the list to execute, produce the corresponding change blocks, and end with a continue block containing the updated task list — marking completed tasks and listing remaining tasks. This cycle repeats until all tasks are complete, at which point a finish block is used instead of a continue block.

The task list should clearly distinguish:
- Completed tasks (e.g., marked with [x] or strikethrough)
- Remaining tasks (e.g., marked with [ ] or unmarked)
- Tasks being executed in the current round

Simple tasks that can be completed within a single response need not be split — generate the full output directly without continue blocks.

**Continue Block Format:**

:::continue <boundary>
<next user message content>
:::end <boundary>

**Rules:**
- Use a continue block when the task cannot be completed in a single response (e.g., due to output length limits or multi-step workflows). The body contains the next user message that will be fed back into the system to continue the task. For multi-round tasks, the body is the updated task list showing completed and remaining tasks.
- A response MUST contain either a finish block (if the task is complete) or a continue block (if more work is needed), but NOT both.
- The continue block must be the last block in the response; no change blocks or other blocks may appear after it.
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
