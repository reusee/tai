package blocks

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/security"
)

// TheoryOfShellBlocks documents the shell block kind: its execution model,
// its turn-based delivery semantics, and the two gates every command
// passes before it runs.
const TheoryOfShellBlocks = `
Shell blocks execute shell commands in a subprocess with a timeout, capture
both stdout and stderr, and return them as user content in the next
generation round. The working directory is the project root. This enables
the model to run tests, check build status, explore the codebase, and verify
its own changes without human intervention. Shell execution is disabled by
default for safety: the -shell flag enables it, and a configured command
allowlist enables it by itself, because the user who lists commands has
already decided to let the model run them (see TheoryOfShellAllowlist).

The turn-based semantics are the critical design constraint: shell output is
delivered only in the NEXT round, never in the response that contains the
shell blocks, so a model that acts on results it has not yet received
fabricates outputs and creates pointless loops (see
TheoryOfDeferredExecution). ShellBlockPrompt is itself the theory text for
the waiting rules — command independence within a response, the prohibition
on change or ingest content that depends on the output, and the summary-first
stop rule — and for the policy the session runs under; neither is repeated
here.

Two gates precede execution: the allowlist, which is the user's own
permission decision and never widens what may run, and the structural
validator (security.ValidateShellCommand), which applies to every command
including the listed ones. See TheoryOfShellAllowlist and
security.TheoryOfShellSecurity.
`

// ShellBlockSystemPrompt is the shell block prompt of a session without a
// configured allowlist: any program may run, filtered only by the
// structural rules. See ShellBlockPrompt and TheoryOfShellAllowlist.
const ShellBlockSystemPrompt = shellPromptHead + shellAnyProgramPolicy + shellPromptTail

const shellTimeout = 30 * time.Second

// executeShellCommand runs a shell command with a timeout derived from the
// provided context and returns the combined stdout/stderr output with a
// status prefix.
func executeShellCommand(ctx context.Context, cmdStr string) string {
	cancelCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(cancelCtx, "sh", "-c", cmdStr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Sprintf("Command failed with error: %v\nStdout:\n%s\nStderr:\n%s", err, stdout.String(), stderr.String())
	}
	return fmt.Sprintf("Command succeeded.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
}

// ProcessShellBlocks executes all shell blocks and returns the outputs as
// generator parts. Only blocks with Kind "shell" are processed; blocks of
// other kinds are skipped. Each command passes two gates before execution:
// the allowlist — an unconfigured list allows every command, a configured
// one allows exactly its entries — and the structural validator; the
// allowlist narrows what may run and never widens it. A rejected command
// returns a message listing the allowed commands as user content instead
// of being executed. Each output part ends with a blank line so
// consecutive parts in the same round stay paragraph-separated after
// verbatim part concatenation; see
// generators.TheoryOfContentUnitSeparation. The provided context allows
// callers to cancel long-running commands. See
// security.TheoryOfShellSecurity and TheoryOfShellAllowlist.
func ProcessShellBlocks(blocks []Block, ctx context.Context, allowed AllowedShellCommands) ([]generators.Part, error) {
	if len(blocks) == 0 {
		return nil, nil
	}
	var parts []generators.Part
	for _, block := range blocks {
		if block.Kind != "shell" {
			continue
		}
		cmdStr := block.Body
		if !allowed.Allows(cmdStr) {
			parts = append(parts, generators.Text(
				allowed.rejectionText(cmdStr),
			))
			continue
		}
		if err := security.ValidateShellCommand(cmdStr); err != nil {
			parts = append(parts, generators.Text(
				fmt.Sprintf("Shell command rejected: %s\n\nError: %v\n\n", cmdStr, err),
			))
			continue
		}
		output := executeShellCommand(ctx, cmdStr)
		parts = append(parts, generators.Text(
			fmt.Sprintf("Shell command: %s\n\n%s\n\n", cmdStr, output),
		))
	}
	return parts, nil
}
