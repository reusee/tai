package codes

import (
	"github.com/reusee/prompts"
)

const TheoryOfReadOnlyFiles = `
The read-only annotation on file context markers — "(read-only)" for text files
and ", read-only" for binary files — signals that a file resides outside the
project tree and must not be modified. The system prompt translates this
filesystem-level annotation into an explicit behavioral constraint on the
model: change blocks must not target any path marked read-only.
`

const ReadOnlyFilesSystemPrompt = `**Read-Only Files:**

Files whose markers include "(read-only)" or ", read-only" reside outside the
project tree and are provided for reference only. They are typically introduced
via symbolic links to external locations.

**Rules:**
- Do NOT emit change blocks (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE, WRITE,
  RENAME) whose file-path refers to a read-only file.
- Use read-only file contents to inform changes to writable project files,
  but never attempt to modify the read-only files themselves.
- If a task requires modifying a read-only file, state this in prose and
  explain the rationale, but do not emit a change block for it.
`

const TheoryOfMandatoryPlanning = `
Mandatory planning requires every task to begin with an overall plan and task
decomposition, emitted as a plan-only first round, followed by execution
rounds delimited by continue blocks. The motivation is output-length safety:
models with long reasoning chains can exceed the maximum generation length on
large or complex tasks, truncating the response mid-block and wasting the
round. A plan-only first round followed by small execution rounds keeps each
round's thinking and output bounded, so no single response approaches the
generation limit. The mandate is opt-in via the -plan flag; when disabled (the
default), the planning system prompt is omitted. When enabled, the mandate
applies uniformly to every task, including apparently trivial ones, because
truncation risk cannot be reliably predicted from the request alone; the cost
is one extra short round-trip per task.

Planning is an extension layered on top of the continue block mechanism (see
TheoryOfContinueBlocks): the mechanism only transports the block body back as
the next user message, while this mandate defines what the body contains and
how rounds are structured. The two are orthogonal: continue blocks may serve
other extensions with entirely different body conventions.

For complex tasks, the model maintains a task list in the continue block body.
In each round, the model selects one or more tasks from the list to execute,
produces the corresponding change blocks, and ends with a continue block
containing the updated task list — marking completed tasks and listing
remaining tasks. This cycle repeats until all tasks are complete, at which
point a summary block is used instead.

Decomposition must precede any action, including analysis and reasoning, not
just code changes. A composite task such as "find bugs and fix" contains an
analysis phase (finding bugs) that may itself span many files or modules and
exceed the generation limit if performed in a single round. The planning round
must therefore partition the input space — for example by file or module group
— so that each round analyzes or acts on only a subset.
`

const TheoryOfTaskDecomposition = `
Task decomposition is a portfolio of strategies applied in combination. No one
strategy suffices for all tasks; the model must select and blend strategies
based on task shape, risk profile, and observed progress.

Structural strategies determine how work is divided: by input partition (e.g.,
one round per focus-file group), by logical step sequence, by architectural
layer (interface definition before implementation before caller updates), or by
independent units (decoupling tasks with no mutual dependencies to maximize
parallel execution).

Adaptive strategies determine how the model responds during generation:
truncate immediately and continue in the next round if output is already long;
refine the task list progressively if a coarse task reveals finer subtasks;
dedicate the next round to diagnosis and fix after an error; adjust the task
list dynamically when execution feedback contradicts the plan.

Quality strategies determine how correctness is ensured: include dedicated
verification rounds after implementation; isolate high-uncertainty tasks as
probing rounds; gather context before making changes.

Scheduling strategies determine ordering and sizing: order by dependency;
isolate high blast-radius changes; split by estimated token consumption;
distinguish one-way door (irreversible) from two-way door (reversible)
decisions.

The primary trigger for splitting into multiple rounds is the expected output
volume of a single round: when a task requires more than approximately 5-7
change blocks, or when an analysis phase must process many files or modules.
Secondary triggers include natural phase boundaries and dependency chains.
Each round should produce a coherent, reviewable set of changes; prefer fewer,
larger rounds over many tiny rounds to minimize round-trip overhead.

Decomposition must precede any action, including analysis and reasoning. A
composite task such as "find bugs and fix" contains an analysis phase that may
itself span many files or modules; the planning round must partition the input
space so that each round — whether analyzing or implementing — handles only a
subset.
`

const MandatoryPlanningSystemPrompt = `**Mandatory Planning and Multi-Round Generation:**

Every task MUST be planned before any change blocks are emitted, and executed
across multiple generation rounds delimited by continue blocks. This mandate
exists because long reasoning chains can exceed the maximum generation length
on large or complex tasks; splitting the work keeps each round's thinking and
output bounded so that no single response approaches the limit.

**Rules:**
- The first response to any task MUST be an overall plan: analyze the request,
  decompose it into a concrete task list, and note dependencies between tasks.
  Emit NO change blocks in the planning round. For small tasks the plan can be
  brief — a short task list is sufficient.
- The planning round MUST end with a continue block containing the task list,
  never only a summary block, because no changes have been produced yet.
- During planning, select and blend task decomposition strategies (see the
  Task Decomposition Strategies section below) to produce the initial task
  list. No single strategy suffices; blend structural, adaptive, quality, and
  scheduling strategies based on task shape and risk.
- Decomposition MUST precede any action, including analysis and reasoning, not
  just change blocks. For composite tasks like "find bugs and fix", the
  analysis phase itself may span many files or modules; the planning round must
  partition the input space (e.g., by file or module group) so each round
  handles only a subset. Do not perform analysis first and then decompose only
  the fix phase — the analysis round itself may exceed the generation limit.
- Each subsequent round executes one or a few tasks from the list, then ends
  with a continue block carrying the updated task list (completed tasks marked,
  remaining tasks listed), until all tasks are complete.
- Keep each execution round small. When in doubt, split finer: more rounds
  with less output per round is always safer than fewer rounds that risk
  truncation.
- The final round ends with a summary block instead of a continue block.
- This mandate applies to EVERY task, including apparently trivial ones, and
  supersedes any guidance elsewhere that permits completing simple tasks in a
  single response without a continue block.

**When to Split Into Multiple Rounds:**
- The task requires more than approximately 5-7 change blocks, which may exceed
  a single response's practical output capacity. Counting the expected change
  blocks before starting is the most reliable trigger.
- The task naturally decomposes into independent phases with clear boundaries
  (e.g., "refactor the interface" followed by "update all callers"). Each phase
  becomes a separate round.
- Later steps depend on the results or review of earlier steps, making
  incremental delivery safer than producing all changes at once.
- The estimated total output (code bodies plus explanatory prose) would
  approach or exceed the model's per-response limit, risking truncation.
- The task includes an analysis phase that must process many files or modules;
  see the decomposition-precedes-action rule above.

Because every task begins with a planning round, these triggers are always
evaluated during planning rather than used to decide whether to use continue
blocks at all.

**Round Granularity:**
Each round should produce a coherent, reviewable set of changes. Prefer fewer,
larger rounds over many tiny rounds to reduce round-trip overhead. A round
that produces only one trivial change block wastes the continue mechanism;
group related changes into the same round.

**Task Decomposition Strategies:**

Task decomposition is not a single algorithm but a portfolio of strategies
applied in combination. No one strategy suffices for all tasks; select and
blend strategies based on task shape, risk profile, and observed progress. The
strategies fall into four categories.

Structural strategies determine how work is divided:
- Input-driven: split by input partition (e.g., one round per focus-file
  group), so each round handles a coherent subset of the input.
- Logical-step-driven: split by explicit sequential steps when the task has a
  clear order of operations.
- Interface-first: for architectural changes, split as "define interface,
  implement, update callers" so each layer is reviewed before the next depends
  on it.
- Independence-driven: split into independent, order-agnostic units of work
  with no mutual dependencies, maximizing decoupling and enabling parallel or
  out-of-order execution.

Adaptive strategies determine how the model responds during generation:
- Output-length-driven: if output is already long (reasoning or code),
  truncate immediately and continue in the next round rather than risking
  mid-block truncation.
- Progressive refinement: if a round reveals that a coarse task hides finer
  subtasks, refine the task list progressively in subsequent rounds.
- Error recovery: after an error, dedicate the next round to diagnosis and fix
  before continuing the original plan.
- Feedback-driven: adjust the task list after each round based on execution
  feedback; decomposition is a living hypothesis, not a fixed decree.

Quality strategies determine how correctness is ensured:
- Verification-driven: include dedicated verification rounds (tests, build
  checks) after implementation, before declaring the task complete.
- Risk-driven: isolate high-uncertainty tasks as probing rounds that validate
  critical assumptions before committing to a path.
- Context-collection-first: gather context (via request-context blocks) before
  making changes, so the model never acts on unverified assumptions.

Scheduling strategies determine ordering and sizing:
- Dependency-driven: order by task dependencies so depended-upon tasks execute
  first.
- Blast-radius-driven: isolate high-impact changes to separate rounds so their
  effects can be reviewed independently.
- Token-budget-driven: split by estimated token consumption so no round
  approaches the output limit.
- Reversibility-driven: distinguish one-way door (irreversible) from two-way
  door (reversible) decisions; give one-way doors a separate round with
  pre-validation.

The planning round applies structural and scheduling strategies to produce the
initial task list. Subsequent rounds apply adaptive and quality strategies as
execution reveals new information. The continue block body carries the
evolving task list, so decomposition is visible, reviewable, and correctable
across rounds.

**Task List Format:**
The task list should clearly distinguish:
- Completed tasks (e.g., marked with [x] or strikethrough)
- Remaining tasks (e.g., marked with [ ] or unmarked)
- Tasks being executed in the current round
`

type SystemPrompt string

func (Module) SystemPrompt(
	comps CodesComponents,
) (ret SystemPrompt) {
	// The base prompt (prompts.Codes) is prepended directly. All
	// block-format, component, and extra prompts come from
	// comps.PromptSections(). Restate prompts are placed at the end of
	// the user prompt via ComponentSet.UserPromptParts(), not in the
	// system prompt.
	// See TheoryOfCodesComponents.
	return SystemPrompt(prompts.Codes + "\n" +
		comps.PromptSections())
}
