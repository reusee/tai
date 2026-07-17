package codes

import (
	"github.com/reusee/prompts"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/configs"
)

const TheoryOfReadOnlyFiles = `
The read-only annotation on file context markers — "(read-only)" for text files
and ", read-only" for binary files — signals that a file resides outside the
project tree and must not be modified. The system prompt translates this
filesystem-level annotation into an explicit behavioral constraint on the
model: change blocks must not target any path marked read-only. This closes
the loop between the annotation produced by the file provider (see
TheoryOfReadOnlySymlinks in anytexts) and the model's generation behavior.
Without an explicit prompt-level rule, the model may attempt to modify
read-only files based on their content, leading to apply errors or
unintended writes outside the project boundary. The rule is stated once in
the system prompt rather than per-file to keep the prompt compact and
cacheable.
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
generation limit. The mandate applies uniformly to every task, including
apparently trivial ones, because truncation risk cannot be reliably predicted
from the request alone; the cost is one extra short round-trip per task. This
refines the continue block guidance: the exemption that allowed simple tasks
to complete in a single response without a continue block is superseded.

The planning round applies structural and scheduling decomposition strategies
(see TheoryOfTaskDecomposition) to produce the initial task list, while
subsequent execution rounds apply adaptive and quality strategies as execution
reveals new information. This makes decomposition a living hypothesis refined
across rounds rather than a one-shot guess.

Decomposition must precede any action, including analysis and reasoning, not
just code changes. A composite task such as "find bugs and fix" contains an
analysis phase (finding bugs) that may itself span many files or modules and
exceed the generation limit if performed in a single round. The planning round
must therefore partition the input space — for example by file or module group
— so that each round analyzes or acts on only a subset. Decomposing only the
fix phase after completing the analysis phase in one unbounded round defeats
the purpose: the analysis round itself may truncate, losing bugs and wasting
the round.
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
  never a finish block, because no changes have been produced yet.
- During planning, select and blend task decomposition strategies (see the
  Task Decomposition Strategies section under Continue Block Kind) to produce
  the initial task list. No single strategy suffices; blend structural,
  adaptive, quality, and scheduling strategies based on task shape and risk.
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
- The final round ends with a finish block instead of a continue block.
- This mandate applies to EVERY task, including apparently trivial ones, and
  supersedes any guidance elsewhere that permits completing simple tasks in a
  single response without a continue block.
`

type ExtraSystemPrompt string

var _ configs.Configurable = ExtraSystemPrompt("")

func (e ExtraSystemPrompt) TaigoConfigurable() {}

func (Module) ExtraSystemPrompt(
	loader configs.Loader,
) ExtraSystemPrompt {
	return configs.First[ExtraSystemPrompt](loader, "extra_system_prompt")
}

type SystemPrompt string

func (Module) SystemPrompt(
	codeProvider codetypes.CodeProvider,
	diffHandler codetypes.DiffHandler,
	dynamicContext DynamicContext,
	shell Shell,
	extra ExtraSystemPrompt,
) (ret SystemPrompt) {
	prompt := prompts.Codes + "\n" +
		codeProvider.SystemPrompt() + "\n" +
		diffHandler.SystemPrompt() + "\n" +
		blocks.FinishBlockSystemPrompt + "\n" +
		ReadOnlyFilesSystemPrompt + "\n" +
		blocks.ContinueBlockSystemPrompt + "\n" +
		MandatoryPlanningSystemPrompt + "\n"
	if bool(dynamicContext) {
		prompt += blocks.RequestContextSystemPrompt + "\n"
	}
	if bool(shell) {
		prompt += blocks.ShellBlockSystemPrompt + "\n"
	}
	prompt += string(extra)
	return SystemPrompt(prompt)
}
