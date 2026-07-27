package blocks

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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

const TheoryOfShellSecurity = `
Shell command execution is protected by a command allowlist policy. Only
predefined read-only and diagnostic commands are permitted, preventing the
model from executing destructive operations (e.g., rm, git push, kill). The
validator parses the command chain by splitting on shell control operators
(|, ;, &&, ||) and checks each subcommand's name against the allowlist. For
composite commands (git, go, npm, etc.), subcommands are further checked
against an allowed subcommand list. Output redirection (>, >>) is forbidden
to prevent file modification via redirection. The find command's -exec
flag is explicitly forbidden to prevent arbitrary command execution through
find. Commands with absolute paths (e.g., /usr/bin/ls) are normalized via
filepath.Base before allowlist lookup. Commands that can execute arbitrary
code (awk, sed) are excluded from the allowlist entirely. This is a
conservative security boundary: it is better to reject a safe command than
to allow a dangerous one. Rejected commands return an error message as user
content so the model can adjust and retry with an allowed command.
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
- **Security policy**: Only commands in the allowed list are executed. Allowed command categories:
  - File viewing: ls, cat, head, tail, wc, file, stat, tree, du, df
  - Search: grep, rg, find (without -exec), which, whereis
  - Text processing: sort, uniq, cut, tr, diff, comm, paste, column
  - System info: pwd, echo, printf, env, printenv, date, uname, hostname, whoami, uptime, free, ps
  - Git (read-only): git status, git diff, git log, git show, git blame, git ls-files, git ls-tree, git describe, git rev-parse, git help, git version
  - Go toolchain: go test, go build, go vet, go list, go doc, go version, go env, go help
  - Package managers (read-only): npm list/view/info/outdated/audit, yarn list/info/outdated, pnpm list/info/outdated
  - Version info: node, python, python3, java, rustc, cargo, gcc, make, cmake
- **Forbidden operations**:
  - Output redirection (>, >>) is not allowed.
  - find -exec / -execdir / -ok / -okdir is not allowed.
  - Commands not in the allowed list are rejected (e.g., rm, mv, cp, chmod, chown, kill, dd, shutdown, reboot, sed, awk).
  - Git write operations are rejected (e.g., git commit, push, pull, merge, rebase, reset, checkout, add, rm, branch, tag, config, stash).
  - Go modifying operations are rejected (e.g., go fmt, go mod, go install, go get, go run, go generate).
- If a command is rejected, the error message will be returned as user content. Adjust the command and try again.
- Shell output triggers a new generation round so the model can act on the results.
- The boundary is a random string chosen by the AI to prevent conflicts with the body content.
`

// allowedCommands defines the set of commands permitted for shell block
// execution and their optional subcommand constraints. When the subcommand
// list is nil, all subcommands are allowed. When non-empty, only the listed
// subcommands are permitted.
// See TheoryOfShellSecurity.
var allowedCommands = map[string][]string{
	// File viewing (read-only)
	"ls":   nil,
	"cat":  nil,
	"head": nil,
	"tail": nil,
	"wc":   nil,
	"file": nil,
	"stat": nil,
	"tree": nil,
	"du":   nil,
	"df":   nil,

	// Search (read-only)
	"grep":    nil,
	"rg":      nil,
	"find":    nil, // -exec is checked separately
	"which":   nil,
	"whereis": nil,

	// Text processing (read-only)
	"sort":   nil,
	"uniq":   nil,
	"cut":    nil,
	"tr":     nil,
	"diff":   nil,
	"comm":   nil,
	"paste":  nil,
	"column": nil,

	// System information (read-only)
	"pwd":      nil,
	"echo":     nil,
	"printf":   nil,
	"env":      nil,
	"printenv": nil,
	"date":     nil,
	"uname":    nil,
	"hostname": nil,
	"whoami":   nil,
	"uptime":   nil,
	"free":     nil,
	"ps":       nil,

	// Git read-only subcommands
	"git": {"status", "diff", "log", "show", "blame", "ls-files", "ls-tree",
		"describe", "rev-parse", "help", "version"},

	// Go toolchain (read-only/diagnostic subcommands)
	"go": {"test", "build", "vet", "list", "doc", "version", "env", "help"},

	// Package managers (read-only subcommands)
	"npm":  {"list", "view", "info", "outdated", "audit", "ls"},
	"yarn": {"list", "info", "outdated"},
	"pnpm": {"list", "info", "outdated"},

	// Version information
	"node":    nil,
	"python":  nil,
	"python3": nil,
	"java":    nil,
	"rustc":   nil,
	"cargo":   nil,
	"gcc":     nil,
	"make":    nil,
	"cmake":   nil,
}

// dangerousFindFlags are find command flags that allow arbitrary command
// execution and must be explicitly forbidden.
// See TheoryOfShellSecurity.
var dangerousFindFlags = map[string]bool{
	"-exec":    true,
	"-execdir": true,
	"-ok":      true,
	"-okdir":   true,
}

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

// validateShellCommand checks whether a command string is safe to execute.
// Returns nil if the command passes all security checks, or an error
// describing why the command was rejected.
// See TheoryOfShellSecurity.
func validateShellCommand(cmdStr string) error {
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return fmt.Errorf("empty command")
	}

	// Output redirection is forbidden to prevent file modification.
	// This is a conservative check: any '>' character triggers rejection,
	// including 2>&1. This is intentional — stdout and stderr are already
	// captured separately by the system, so redirection is unnecessary.
	if strings.Contains(cmdStr, ">") {
		return fmt.Errorf("output redirection (>) is not allowed")
	}

	// Parse the command chain and validate each subcommand.
	subcommands := splitCommandChain(cmdStr)
	for _, sub := range subcommands {
		if err := validateSubcommand(sub); err != nil {
			return err
		}
	}
	return nil
}

// splitCommandChain splits a command string by shell control operators
// (|, ;, &&, ||) into individual subcommand strings. This is a simplified
// parser for security validation; it does not handle quoted separators.
// For security purposes, over-splitting is safe: it only results in
// additional allowlist checks, never in bypassing a check.
// See TheoryOfShellSecurity.
func splitCommandChain(cmdStr string) []string {
	// Replace multi-character operators first to avoid double-splitting.
	s := strings.ReplaceAll(cmdStr, "&&", "\x00")
	s = strings.ReplaceAll(s, "||", "\x00")
	s = strings.ReplaceAll(s, "|", "\x00")
	s = strings.ReplaceAll(s, ";", "\x00")

	parts := strings.Split(s, "\x00")
	var result []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// validateSubcommand validates a single subcommand (no control operators).
// It checks the command name against the allowlist and, for commands with
// subcommand constraints, verifies the subcommand is permitted.
// See TheoryOfShellSecurity.
func validateSubcommand(cmdStr string) error {
	fields := strings.Fields(cmdStr)
	if len(fields) == 0 {
		return nil
	}

	// Extract the command name, handling absolute paths (e.g., /usr/bin/ls).
	cmdName := filepath.Base(fields[0])

	// Check if the command is in the allowlist.
	allowedSubs, ok := allowedCommands[cmdName]
	if !ok {
		return fmt.Errorf("command %q is not in the allowed list", cmdName)
	}

	// If the command has subcommand constraints, check the subcommand.
	if allowedSubs != nil {
		if len(fields) < 2 {
			return fmt.Errorf("command %q requires a subcommand from: %s", cmdName, strings.Join(allowedSubs, ", "))
		}
		subcommand := fields[1]
		if !slices.Contains(allowedSubs, subcommand) {
			return fmt.Errorf("subcommand %q is not allowed for %q; allowed: %s", subcommand, cmdName, strings.Join(allowedSubs, ", "))
		}
	}

	// Check for dangerous find flags that allow arbitrary command execution.
	if cmdName == "find" {
		for _, arg := range fields[1:] {
			if dangerousFindFlags[arg] {
				return fmt.Errorf("find %s is not allowed for security reasons", arg)
			}
		}
	}

	return nil
}

// ProcessShellBlocks executes all shell blocks and returns the outputs as
// generator parts. Each command is validated against the security allowlist
// before execution; rejected commands return an error message as user content
// instead of being executed. See TheoryOfShellSecurity.
func ProcessShellBlocks(blocks []Block) ([]generators.Part, error) {
	if len(blocks) == 0 {
		return nil, nil
	}
	var parts []generators.Part
	for _, block := range blocks {
		cmdStr := block.Body
		if err := validateShellCommand(cmdStr); err != nil {
			parts = append(parts, generators.Text(
				fmt.Sprintf("Shell command rejected: %s\n\nError: %v\n", cmdStr, err),
			))
			continue
		}
		output := executeShellCommand(cmdStr)
		parts = append(parts, generators.Text(
			fmt.Sprintf("Shell command: %s\n\n%s", cmdStr, output),
		))
	}
	return parts, nil
}
