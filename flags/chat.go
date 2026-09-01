package flags

import (
	"fmt"
	"io"
	"os"
	"slices"

	"golang.org/x/term"
)

const TheoryOfStdinFlag = `
The -stdin flag adds the content of standard input to the chat messages
(Chats). It is implemented as an additional key on the Chats flag type itself,
alongside "chat". This composes correctly with chat flags regardless of
argument order: each Handle invocation reads the current Chats value from the
scope (which includes all previously parsed chat and stdin flags), appends its
contribution, and forks an updated pointer. Standard input is read at flag
parse time, so the content is captured exactly once. If standard input is a
terminal, no content is read and the current Chats value is forked unchanged.
`

const TheoryOfCleanFlag = `
The clean flag appends a fixed cleanup prompt to the chat messages (Chats).
The prompt instructs the model to delete redundant code and mechanisms, to
merge and simplify duplicate tests, and to delete theory text content that
duplicates other theory texts. It is an additional key on the Chats
flag type, registered as the bare word "clean" like "chat"; Parse matches
arguments verbatim, so the invocation carries no leading dash. It composes
with chat flags in any argument order: each Handle invocation reads the
current Chats value from the scope, appends its contribution, and forks an
updated pointer. The flag takes no argument; the prompt is fixed.
`

const TheoryOfAlignFlag = `
The align flag appends a fixed alignment-check prompt to the chat messages
(Chats). The prompt instructs the model to check whether the system theory
(theory constants and specifications) matches the implementation. On
inconsistency, the model writes the report to _alignment.md and touches
nothing else: an inconsistency may come from outdated theory or from a
faulty implementation, and only the user can decide which side to correct.
When everything matches, _alignment.md is not created or changed, so an
absent or untouched file means the theory and the implementation align.
It is an additional key on the Chats flag type, registered as the bare
word "align" like "chat"; Parse matches arguments verbatim, so the
invocation carries no leading dash. It composes with chat flags in any
argument order: each Handle invocation reads the current Chats value from
the scope, appends its contribution, and forks an updated pointer. The
flag takes no argument; the prompt is fixed.
`

const TheoryOfDistillFlag = `
The distill flag appends a fixed theory-distillation prompt to the chat
messages (Chats). The prompt instructs the model to distill the project's
theory — ideas, models, decisions, logical interfaces, and constraints —
from the whole down to its parts, in great detail, so that the theory text
alone is sufficient to rebuild the entire project, and to write the result
to _theory.go. File and directory structure are excluded: the theory is the
design rationale, not a tree listing. The work is split into steps: the
model first produces the overall skeleton and writes it to the file, then
refines it step by step. It is an additional key on the Chats flag type,
registered as the bare word "distill" like "chat"; Parse matches arguments
verbatim, so the invocation carries no leading dash. It composes with chat
flags in any argument order: each Handle invocation reads the current Chats
value from the scope, appends its contribution, and forks an updated
pointer. The flag takes no argument; the prompt is fixed.
`

// cleanPrompt is the fixed task directive the clean flag appends to the
// chat messages. See TheoryOfCleanFlag.
const cleanPrompt = "Delete redundant code and mechanisms. Merge and simplify duplicate tests. Delete theory text content that duplicates other theory texts."

// alignPrompt is the fixed task directive the align flag appends to the
// chat messages. See TheoryOfAlignFlag.
const alignPrompt = "Check whether the system theory (theory constants and specifications) matches the implementation. On inconsistency, write the report to _alignment.md without modifying anything else: the inconsistency may come from outdated theory or from a faulty implementation, and the user decides how to resolve it. When everything matches, do not create or change _alignment.md."

// distillPrompt is the fixed task directive the distill flag appends to the
// chat messages. See TheoryOfDistillFlag.
const distillPrompt = "Distill the project's theory, from the whole down to its parts, in great detail, so that a model can rebuild the entire project from the theory text alone. Capture ideas, models, decisions, logical interfaces, and constraints; do not describe files or directory structures. Write the result to _theory.go. Work in steps: first produce the overall skeleton and write it to the file, then refine it step by step."

type Chats []string

func (Module) Chats() (ret Chats) {
	return
}

var _ Flag = Chats(nil)

func (c Chats) Keys() map[string]string {
	return map[string]string{
		"chat":    "Add a chat message to the conversation",
		"-stdin":  "Add standard input content to the chat messages",
		"clean":   "Add the code cleanup prompt to the chat messages",
		"align":   "Add the theory-implementation alignment check prompt to the chat messages",
		"distill": "Add the theory distillation prompt to the chat messages",
	}
}

func (c Chats) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	switch key {
	case "chat":
		if len(args) == 0 {
			return nil, nil, fmt.Errorf("expecting string argument, got empty")
		}
		ret := append(slices.Clone(c), args[0])
		return &ret, args[1:], nil
	case "-stdin":
		content := readStdinContent()
		if len(content) == 0 {
			return &c, args, nil
		}
		ret := append(slices.Clone(c), string(content))
		return &ret, args, nil
	case "clean":
		ret := append(slices.Clone(c), cleanPrompt)
		return &ret, args, nil
	case "align":
		ret := append(slices.Clone(c), alignPrompt)
		return &ret, args, nil
	case "distill":
		ret := append(slices.Clone(c), distillPrompt)
		return &ret, args, nil
	}
	panic("key not handle: " + key)
}

// readStdinContent reads all of standard input if it is not a terminal.
// Returns nil if stdin is a terminal or if reading fails.
func readStdinContent() []byte {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil
	}
	return content
}
