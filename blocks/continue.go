package blocks

import (
	"github.com/reusee/tai/generators"
)

const TheoryOfTaskDecomposition = `
Task decomposition is not a single algorithm but a portfolio of strategies
applied in combination. No one strategy suffices for all tasks; the model must
select and blend strategies based on task shape, risk profile, and observed
progress. The strategies fall into four categories.

Structural strategies determine how work is divided: by input partition (e.g.,
one round per focus-file group), by logical step sequence, or by architectural
layer (interface definition before implementation before caller updates).
Structural strategies produce the initial task list during the planning round.

Adaptive strategies determine how the model responds during generation: if
output is already long (reasoning or code), truncate immediately and continue
in the next round rather than risking truncation; if a round reveals that a
coarse task hides finer subtasks, refine the task list progressively; if an
error occurs, dedicate the next round to diagnosis and fix; if execution
feedback contradicts the plan, adjust the task list dynamically. Adaptive
strategies make decomposition a living hypothesis, not a fixed decree.

Quality strategies determine how correctness is ensured: include dedicated
verification rounds (tests, build checks) after implementation; isolate
high-uncertainty tasks as probing rounds that validate critical assumptions
before committing to a path; gather context before making changes so the
model never acts on unverified assumptions. Quality strategies front-load
risk reduction.

Scheduling strategies determine ordering and sizing: order by dependency so
depended-upon tasks execute first; isolate high blast-radius changes to
separate rounds; split by estimated token consumption so no round approaches
the output limit; distinguish one-way door (irreversible) from two-way door
(reversible) decisions, giving one-way doors a separate round with
pre-validation. Scheduling strategies keep each round safe and bounded.

The mandatory planning round (see TheoryOfMandatoryPlanning) applies structural
and scheduling strategies to produce the initial task list. Subsequent rounds
apply adaptive and quality strategies as execution reveals new information.
The continue block body carries the evolving task list, so decomposition is
visible, reviewable, and correctable across rounds.

Decomposition must precede any action, including analysis and reasoning, not
just code changes. A composite task such as "find bugs and fix" contains an
analysis phase that may itself span many files or modules; if decomposition is
applied only to the fix phase after a single unbounded analysis round, that
analysis round may exceed the generation limit and truncate, losing findings
and wasting the round. The planning round must therefore partition the input
space so that each round — whether analyzing or implementing — handles only a
subset of the inputs.
`

const TheoryOfContinueBlocks = `
Continue blocks allow the model to self-drive multi-turn generation by emitting
a continue block at the end of a response when the task is not yet complete.
The system parses the continue block, extracts its body as the next user message,
and automatically starts a new generation round. This enables the model to
produce arbitrarily long outputs by chaining multiple rounds. Each round must
end with either a finish block (task complete) or a continue block (more work
needed), but not both.

The primary trigger for using continue blocks is the expected output volume of
a single round: when a task requires more than approximately 5-7 change blocks,
or when an analysis phase must process many files or modules, the model should
decompose it into multiple rounds. Analysis-heavy tasks are a primary trigger
because the analysis itself — before any change blocks are produced — can
exceed the generation limit if performed in a single round. Secondary triggers
include natural phase boundaries (e.g., interface refactoring followed by
caller updates) and dependency chains where later steps depend on earlier
results. Each round should produce a coherent, reviewable set of changes; prefer
fewer, larger rounds over many tiny rounds to minimize round-trip overhead.

For complex tasks, the model maintains a task list in the continue block body.
In each round, the model selects one or more tasks from the list to execute,
produces the corresponding change blocks, and ends with a continue block
containing the updated task list — marking completed tasks and listing
remaining tasks. This cycle repeats until all tasks are complete, at which
point a finish block is used instead. This avoids hitting the single-request
generation limit and keeps each round focused and reviewable.

The mandatory planning mandate (see TheoryOfMandatoryPlanning) requires every
task to begin with a planning round that emits a continue block, so the
triggers above are always evaluated during planning rather than used to decide
whether to use continue blocks at all. Task decomposition itself follows the
portfolio of strategies documented in TheoryOfTaskDecomposition: no single
strategy suffices, and the model must blend structural, adaptive, quality, and
scheduling strategies based on task shape and observed progress. Decomposition
must precede any action including analysis, so the planning round partitions
the input space before the model begins analyzing or implementing.
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
- The task includes an analysis phase (e.g., "find bugs and fix") that must process many files or modules. The analysis itself — before any change blocks are produced — can exceed the generation limit if performed in a single round. Decompose the analysis by input partition (e.g., one round per file or module group) so each round analyzes only a subset. Do not perform the analysis first and then decompose only the fix phase.

The mandatory planning mandate (see Mandatory Planning and Multi-Round Generation in the system prompt) requires every task to begin with a planning round that emits a continue block, so the conditions above are always evaluated during planning rather than used to decide whether to use continue blocks at all.

**Round Granularity:**
Each round should produce a coherent, reviewable set of changes. Prefer fewer, larger rounds over many tiny rounds to reduce round-trip overhead. A round that produces only one trivial change block wastes the continue mechanism; group related changes into the same round.

**Task Decomposition Strategies:**

Task decomposition is not a single algorithm but a portfolio of strategies applied in combination. No one strategy suffices for all tasks; select and blend strategies based on task shape, risk profile, and observed progress. The strategies fall into four categories.

Structural strategies determine how work is divided:
- Input-driven: split by input partition (e.g., one round per focus-file group), so each round handles a coherent subset of the input.
- Logical-step-driven: split by explicit sequential steps when the task has a clear order of operations.
- Interface-first: for architectural changes, split as "define interface, implement, update callers" so each layer is reviewed before the next depends on it.

Adaptive strategies determine how the model responds during generation:
- Output-length-driven: if output is already long (reasoning or code), truncate immediately and continue in the next round rather than risking mid-block truncation.
- Progressive refinement: if a round reveals that a coarse task hides finer subtasks, refine the task list progressively in subsequent rounds.
- Error recovery: after an error, dedicate the next round to diagnosis and fix before continuing the original plan.
- Feedback-driven: adjust the task list after each round based on execution feedback; decomposition is a living hypothesis, not a fixed decree.

Quality strategies determine how correctness is ensured:
- Verification-driven: include dedicated verification rounds (tests, build checks) after implementation, before declaring the task complete.
- Risk-driven: isolate high-uncertainty tasks as probing rounds that validate critical assumptions before committing to a path.
- Context-collection-first: gather context (via request-context blocks) before making changes, so the model never acts on unverified assumptions.

Scheduling strategies determine ordering and sizing:
- Dependency-driven: order by task dependencies so depended-upon tasks execute first.
- Blast-radius-driven: isolate high-impact changes to separate rounds so their effects can be reviewed independently.
- Token-budget-driven: split by estimated token consumption so no round approaches the output limit.
- Reversibility-driven: distinguish one-way door (irreversible) from two-way door (reversible) decisions; give one-way doors a separate round with pre-validation.

The planning round applies structural and scheduling strategies to produce the initial task list. Subsequent rounds apply adaptive and quality strategies as execution reveals new information. The continue block body carries the evolving task list, so decomposition is visible, reviewable, and correctable across rounds.

The task list should clearly distinguish:
- Completed tasks (e.g., marked with [x] or strikethrough)
- Remaining tasks (e.g., marked with [ ] or unmarked)
- Tasks being executed in the current round

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
