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

const ContinueBlockSystemPrompt = `
Continue Block Kind:

The "continue" kind signals that the task is not yet complete and more rounds of generation are needed. It MUST be the last block in the response, after all change blocks.

**Continue Block Format:**

:::continue <boundary>
<next user message content>
:::end <boundary>

**Rules:**
- Use a continue block when the task cannot be completed in a single response (e.g., due to output length limits or multi-step workflows). The body contains the next user message that will be fed back into the system to continue the task.
- A response MUST contain either a finish block (if the task is complete) or a continue block (if more work is needed), but NOT both.
- The continue block must be the last block in the response; no change blocks or other blocks may appear after it.
- The boundary is a random string chosen by the AI to prevent conflicts with the body content.
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
	extra ExtraSystemPrompt,
) (ret SystemPrompt) {
	prompt := prompts.Codes + "\n" +
		codeProvider.SystemPrompt() + "\n" +
		diffHandler.SystemPrompt() + "\n" +
		blocks.FinishBlockSystemPrompt + "\n" +
		ReadOnlyFilesSystemPrompt + "\n" +
		ContinueBlockSystemPrompt + "\n"
	if bool(dynamicContext) {
		prompt += blocks.RequestContextSystemPrompt + "\n"
	}
	prompt += string(extra)
	return SystemPrompt(prompt)
}
