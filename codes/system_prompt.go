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
		blocks.ContinueBlockSystemPrompt + "\n"
	if bool(dynamicContext) {
		prompt += blocks.RequestContextSystemPrompt + "\n"
	}
	if bool(shell) {
		prompt += blocks.ShellBlockSystemPrompt + "\n"
	}
	prompt += string(extra)
	return SystemPrompt(prompt)
}
