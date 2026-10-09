package blocks

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/security"
)

// TheoryOfShellBlocks documents the shell block kind: its execution model,
// its turn-based delivery semantics, and the two gates every command
// passes before it runs.
const TheoryOfShellBlocks = `
Shell blocks execute shell commands in a subprocess, capture both stdout
and stderr, and return them as user content in the next generation round.
The working directory is the project root. A command runs to completion:
no duration limit is enforced. Legitimate work — a full test suite, a
large build — may run for a long time, and a deadline would kill it
mid-run, so the model would read the limit as the command's own failure.
Only the caller's context cancellation ends a command. This enables the
model to run tests, check build status, explore the codebase, and verify
its own changes without human intervention. Shell execution is disabled
by default for safety: the -shell flag enables it, and a configured
command allowlist enables it by itself, because the user who lists
commands has already decided to let the model run them (see
TheoryOfShellAllowlist).

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

The shell block's op parameter extends the kind with background tasks:
background starts a command and returns its task number, and a later
output or kill block addresses it. The task model, its session binding,
and its gate semantics live in TheoryOfShellTasks; the bounded capture
that keeps one command's output from flooding the context lives in
TheoryOfShellOutputCapture. Neither is repeated here.
`

// ShellBlockSystemPrompt is the shell block prompt of a session without a
// configured allowlist: any program may run, filtered only by the
// structural rules. See ShellBlockPrompt and TheoryOfShellAllowlist.
const ShellBlockSystemPrompt = shellPromptHead + shellAnyProgramPolicy + shellPromptTail

// executeShellCommand runs a shell command and returns the combined
// stdout/stderr output with a status prefix. Each stream's capture is
// bounded: output beyond maxShellOutputBytes spills to a temporary file and
// the excerpt names it, so one command cannot flood the context. See
// TheoryOfShellOutputCapture. No duration limit applies: the command runs to
// completion, and the provided context cancels it when the session shuts
// down. See TheoryOfShellBlocks.
func executeShellCommand(ctx context.Context, cmdStr string) string {
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	// A grandchild that outlives the direct child — the child is killed
	// on context cancellation — can hold the output pipes open; WaitDelay
	// bounds the wait so a killed command returns promptly with the
	// output it produced. See TheoryOfShellTasks.
	cmd.WaitDelay = shellWaitDelay
	stdout := newBoundedCapture(maxShellOutputBytes)
	stderr := newBoundedCapture(maxShellOutputBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	stdout.Close()
	stderr.Close()
	if err != nil {
		return fmt.Sprintf("Command failed with error: %v\nStdout:\n%s\nStderr:\n%s", err, stdout.String(), stderr.String())
	}
	return fmt.Sprintf("Command succeeded.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
}

// ProcessShellBlocks executes the shell blocks of one round and returns the
// results as generator parts. Only blocks with Kind "shell" are processed;
// blocks of other kinds are skipped. A block's op attribute selects how its
// command relates to the round: run (the default) executes the command and
// returns its output; background starts it as a session task and returns the
// task number; output collects a task's status and output; kill terminates a
// task. The optional tasks argument carries the session's task registry;
// without it, the task ops report that background tasks are unavailable.
// See TheoryOfShellTasks.
//
// Each executed command passes two gates before it runs: the allowlist — an
// unconfigured list allows every command, a configured one allows exactly
// its entries — and the structural validator; the allowlist narrows what
// may run and never widens it. A rejected command returns a message listing
// the allowed commands as user content instead of being executed. Each
// output part ends with a blank line so consecutive parts in the same round
// stay paragraph-separated after verbatim part concatenation; see
// generators.TheoryOfContentUnitSeparation. The provided context allows
// callers to cancel long-running commands. See
// security.TheoryOfShellSecurity and TheoryOfShellAllowlist.
func ProcessShellBlocks(blocks []Block, ctx context.Context, allowed AllowedShellCommands, tasks ...*ShellTasks) ([]generators.Part, error) {
	if len(blocks) == 0 {
		return nil, nil
	}
	var registry *ShellTasks
	if len(tasks) > 0 {
		registry = tasks[0]
	}
	var parts []generators.Part
	for _, block := range blocks {
		if block.Kind != "shell" {
			continue
		}
		cmdStr := block.Body
		switch op := shellOpOf(block); op {
		case shellOpRun, shellOpBackground:
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
			if op == shellOpBackground {
				if registry == nil {
					parts = append(parts, generators.Text(
						"Shell command rejected: background tasks are not available in this session; run the command with op=run instead.\n\n",
					))
					continue
				}
				id := registry.Start(ctx, cmdStr)
				parts = append(parts, generators.Text(
					fmt.Sprintf("Shell task %d started: %s\n\nCollect it with op=output&task=%d, or stop it with op=kill&task=%d.\n\n", id, cmdStr, id, id),
				))
				continue
			}
			output := executeShellCommand(ctx, cmdStr)
			parts = append(parts, generators.Text(
				fmt.Sprintf("Shell command: %s\n\n%s\n\n", cmdStr, output),
			))
		case shellOpOutput:
			if registry == nil {
				parts = append(parts, generators.Text(
					"Shell output unavailable: background tasks are not available in this session.\n\n",
				))
				continue
			}
			id, ok := shellTaskOf(block)
			if !ok {
				parts = append(parts, generators.Text(
					"Shell output rejected: the task parameter is missing or malformed; pass task=<number>.\n\n",
				))
				continue
			}
			output, ok := registry.Collect(id)
			if !ok {
				parts = append(parts, generators.Text(
					fmt.Sprintf("Shell output rejected: no shell task %d; %s.\n\n", id, shellTaskIDsText(registry.ActiveIDs())),
				))
				continue
			}
			parts = append(parts, generators.Text(
				fmt.Sprintf("Shell task %d output:\n\n%s\n\n", id, output),
			))
		case shellOpKill:
			if registry == nil {
				parts = append(parts, generators.Text(
					"Shell kill unavailable: background tasks are not available in this session.\n\n",
				))
				continue
			}
			id, ok := shellTaskOf(block)
			if !ok {
				parts = append(parts, generators.Text(
					"Shell kill rejected: the task parameter is missing or malformed; pass task=<number>.\n\n",
				))
				continue
			}
			if !registry.Kill(id) {
				parts = append(parts, generators.Text(
					fmt.Sprintf("Shell kill rejected: no shell task %d; %s.\n\n", id, shellTaskIDsText(registry.ActiveIDs())),
				))
				continue
			}
			parts = append(parts, generators.Text(
				fmt.Sprintf("Shell task %d killed.\n\n", id),
			))
		default:
			parts = append(parts, generators.Text(
				fmt.Sprintf("Shell command rejected: unknown op %q; use run, background, output, or kill.\n\n", op),
			))
		}
	}
	return parts, nil
}
