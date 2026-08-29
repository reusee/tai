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
The -clean flag appends a fixed cleanup prompt to the chat messages (Chats).
The prompt instructs the model to delete redundant code and mechanisms, and
to merge and simplify duplicate tests. It is an additional key on the Chats
flag type, like -stdin, and composes with chat flags in any argument order:
each Handle invocation reads the current Chats value from the scope, appends
its contribution, and forks an updated pointer. The flag takes no argument;
the prompt is fixed.
`

// cleanPrompt is the fixed task directive the -clean flag appends to the
// chat messages. See TheoryOfCleanFlag.
const cleanPrompt = "Delete redundant code and mechanisms. Merge and simplify duplicate tests."

type Chats []string

func (Module) Chats() (ret Chats) {
	return
}

var _ Flag = Chats(nil)

func (c Chats) Keys() map[string]string {
	return map[string]string{
		"chat":   "Add a chat message to the conversation",
		"-stdin": "Add standard input content to the chat messages",
		"-clean": "Add the code cleanup prompt to the chat messages",
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
	case "-clean":
		ret := append(slices.Clone(c), cleanPrompt)
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
