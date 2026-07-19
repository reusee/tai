package blocks

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/reusee/tai/generators"
)

const TheoryOfShellBlocks = `
Shell blocks allow the model to execute shell commands and receive the output
as part of the next generation round. This enables the model to run tests,
check build status, explore the codebase, and verify its own changes without
human intervention. Each shell block's command is executed in a subprocess
with a timeout, and both stdout and stderr are captured and returned to the
model as user content. The working directory is the project root.
Shell block execution is disabled by default for safety; the -shell flag
enables it. When enabled, the system prompt includes shell block instructions
so the model knows how to emit shell blocks, and the generation loop executes
any shell blocks found in model output, feeding results back as user content
for the next round.
`

const ShellBlockSystemPrompt = `
Shell Block Kind:

The "shell" kind allows the model to execute shell commands and receive the output as part of the next generation round. This enables the model to run tests, check build status, explore the codebase, and verify changes autonomously.

**Shell Block Format:**

:::<boundary> <shell>
<shell command>
:::<boundary> </shell>

**Rules:**
- Use shell blocks to run tests, check build status, explore the codebase, or verify changes.
- The command is executed with ` + "`" + `sh -c` + "`" + ` in the project root directory.
- Both stdout and stderr are captured and returned as user content in the next round.
- A timeout of 30 seconds is enforced per command.
- Prefer read-only or diagnostic commands (e.g., ls, cat, grep, go test, go build, go vet, git status, git diff, git log). Avoid destructive commands (rm, git push --force, etc.).
- Shell output triggers a new generation round so the model can act on the results.
- The boundary is a random string chosen by the AI to prevent conflicts with the body content.
`

const shellTimeout = 30 * time.Second

// executeShellCommand runs a shell command with a timeout and returns the
// combined stdout/stderr output with a status prefix.
func executeShellCommand(cmdStr string) string {
	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Sprintf("Command failed with error: %v\nStdout:\n%s\nStderr:\n%s", err, stdout.String(), stderr.String())
	}
	return fmt.Sprintf("Command succeeded.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
}

// ProcessShellBlocks executes all shell blocks in the parser state and returns
// the outputs as generator parts to be appended as user content.
// It pops all shell blocks from the block state.
func ProcessShellBlocks(parserState *ParserState) ([]generators.Part, error) {
	if parserState == nil {
		return nil, nil
	}
	shellBlocks := parserState.PopBlocksByKind("shell")
	if len(shellBlocks) == 0 {
		return nil, nil
	}

	var parts []generators.Part
	for _, block := range shellBlocks {
		cmdStr := block.Body
		output := executeShellCommand(cmdStr)
		parts = append(parts, generators.Text(
			fmt.Sprintf("Shell command: %s\n\n%s", cmdStr, output),
		))
	}
	return parts, nil
}
