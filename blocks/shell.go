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

Shell command validation is handled by the security package
(security.ValidateShellCommand), which enforces a command allowlist via
AST-level parsing. See security.TheoryOfShellSecurity for the security model.
`

const ShellBlockSystemPrompt = `
Shell Block Kind:

Use the "shell" kind to execute shell commands and receive the output as part of the next generation round. Use shell blocks to run tests, check build status, explore the codebase, and verify changes autonomously.

**Shell Block Format (complete example):**

<<爞齌 <shell>
go test ./...
爞齌

The delimiter 爞齌 in the example is illustrative only: in every block you emit, choose exactly two uncommon Chinese characters as the delimiter, and use the same delimiter on the closing line. The opening marker must start at the beginning of a line, and the closing line is the delimiter alone on its own line. Never write the placeholder text "DELIMITER" or reuse an example delimiter in a real marker.

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
- Version info: node, python, python3, java (--version/-version only), rustc, cargo (build/test/check/vet/metadata/tree/info/search/clean/doc/fetch only), gcc, make, cmake
- **Forbidden operations**:
  - Output redirection (>, >>) is not allowed.
  - find -exec / -execdir / -ok / -okdir is not allowed.
  - Commands not in the allowed list are rejected (e.g., rm, mv, cp, chmod, chown, kill, dd, shutdown, reboot, sed, awk).
  - Git write operations are rejected (e.g., git commit, push, pull, merge, rebase, reset, checkout, add, rm, branch, tag, config, stash).
- Go modifying operations are rejected (e.g., go fmt, go mod, go install, go get, go run, go generate).
  - Background execution (&) and coprocesses are not allowed.
  - Inline code execution via interpreter flags is not allowed (e.g., python -c, python3 -c, python -m, node -e, node --eval, node -p, node -r).
  - env must not be used to execute commands (e.g., env rm -rf / is rejected).
  - cargo run is not allowed; cargo is restricted to build, test, check, vet, metadata, tree, info, search, clean, doc, fetch.
  - java is restricted to --version and -version only (java -jar and java ClassName execute arbitrary code).
  - go test -exec is not allowed.
  - Heredoc bodies, arithmetic expansion, and parameter expansion are recursively validated for command substitutions.
- If a command is rejected, the error message will be returned as user content. Adjust the command and try again.
- Shell output triggers a new generation round so the model can act on the results.
`

const ShellBlockRestatePrompt = `- Shell block: emit
<<齑靁 <shell>
<shell command>
齑靁
to execute a command. The command runs with sh -c in the project root with a 30-second timeout. Only allowed commands are executed; rejected commands return an error message. Shell output triggers a new generation round. The example delimiter 齑靁 is illustrative: choose two uncommon Chinese characters as the delimiter, the SAME delimiter on the closing line. The opening marker starts at the beginning of a line; the closing line is the delimiter alone. Never write the placeholder text "DELIMITER" or reuse an example delimiter literally.`

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
// other kinds are skipped. Each command is validated against the security
// allowlist before execution; rejected commands return an error message as
// user content instead of being executed. The provided context allows
// callers to cancel long-running commands. See security.TheoryOfShellSecurity.
func ProcessShellBlocks(blocks []Block, ctx context.Context) ([]generators.Part, error) {
	if len(blocks) == 0 {
		return nil, nil
	}
	var parts []generators.Part
	for _, block := range blocks {
		if block.Kind != "shell" {
			continue
		}
		cmdStr := block.Body
		if err := security.ValidateShellCommand(cmdStr); err != nil {
			parts = append(parts, generators.Text(
				fmt.Sprintf("Shell command rejected: %s\n\nError: %v\n", cmdStr, err),
			))
			continue
		}
		output := executeShellCommand(ctx, cmdStr)
		parts = append(parts, generators.Text(
			fmt.Sprintf("Shell command: %s\n\n%s", cmdStr, output),
		))
	}
	return parts, nil
}
