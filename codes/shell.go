package codes

import "github.com/reusee/tai/cmds"

// TheoryOfShellBlocks moved to the blocks package.
// ShellBlockSystemPrompt moved to the blocks package.

// Shell controls whether shell block execution is enabled.
// When true, the system prompt includes shell block instructions,
// and shell blocks from model output are executed with results
// fed back as user content for the next generation round.
// See TheoryOfShellBlocks (in blocks package).
type Shell bool

var shellFlag Shell

func init() {
	cmds.Define("-shell", cmds.Func(func() {
		shellFlag = true
	}).Desc("enable shell command execution from model output"))
}

func (Module) Shell() Shell {
	return shellFlag
}
