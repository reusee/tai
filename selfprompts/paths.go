package selfprompts

import "github.com/reusee/tai/pathutil"

// selfPromptFile names the state file the map lives in, inside the tai
// dir's state directory. See TheoryOfSelfPrompt.
const selfPromptFile = "self-prompt"

// selfPromptFilePath returns the path of the map file inside dir. The name
// is a constant, so the path never leaves the state directory.
// See TheoryOfSelfPrompt.
func selfPromptFilePath(dir pathutil.TaiDir) string {
	return dir.StatePath(selfPromptFile)
}
