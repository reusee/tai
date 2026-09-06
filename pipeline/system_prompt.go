package pipeline

import (
	"strings"

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

const SkeletonFilesSystemPrompt = `**Skeleton Files:**

Files whose markers read "begin of skeleton of file <path>" carry a parsed
structural summary of the file, not the file's actual content. The summary
lists top-level structure (headings or definitions) and omits details.

**Rules:**
- Treat skeleton content as an index of the file's structure, not as source
  text.
- Do not emit change blocks based on skeleton content alone.
- To modify a file shown as a skeleton, or to read any detail the skeleton
  omits, fetch the original file with an ingest block first:
  <file path="..." />.
`

type SystemPrompt string

func (Module) SystemPrompt(
	comps CodesComponents,
) (ret SystemPrompt) {
	// The base prompt (prompts.Codes) is prepended directly. All
	// block-format, component, and extra prompts come from
	// comps.PromptSections(). The system prompt carries no reminder
	// section: the late reminder is the verbatim system prompt restate
	// (components.SystemPromptRestate), appended at the end of the user
	// prompt. The base prompt's trailing whitespace is trimmed and
	// the sections are joined with a blank line, so the base prompt and
	// the first section are always separated by a blank line regardless
	// of the base constant's edge newlines. See TheoryOfCodesComponents
	// and generators.TheoryOfContentUnitSeparation.
	base := strings.TrimRight(prompts.Codes, " \t\n\r")
	sections := comps.PromptSections()
	if sections == "" {
		return SystemPrompt(base + "\n")
	}
	return SystemPrompt(base + "\n\n" + sections)
}
