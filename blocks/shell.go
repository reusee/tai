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
Shell blocks execute shell commands in a subprocess with a timeout, capture both
stdout and stderr, and return them as user content in the next generation round.
The working directory is the project root. This enables the model to run tests,
check build status, explore the codebase, and verify its own changes without human
intervention. Execution is disabled by default for safety; the -shell flag enables
it and adds the shell block instructions to the system prompt.

The turn-based semantics are critical: shell output is delivered only in the NEXT
round, never in the response that contains the shell blocks. The model may emit
multiple shell blocks in one response, but only when their commands are independent
of one another: every shell block in a response executes only after the response
ends, so no shell block can use the output of another shell block from the same
response. Content that depends on shell output — whether a change block or a
read block — must never be emitted in the same response as the shell
blocks it depends on: the model would act on results it has not yet received,
creating pointless loops. After the last shell block, the model emits the
summary block immediately and only then ends the response, waiting for the
results before emitting dependent content; the stop rule is phrased
summary-first so no stop instruction licenses halting at a shell block's
closing line. The system prompt (ShellBlockSystemPrompt) states these rules
explicitly so the model waits for the results before proceeding.

Shell command validation is handled by the security package
(security.ValidateShellCommand), which enforces a command allowlist via AST-level
parsing. See security.TheoryOfShellSecurity for the security model.
`

const ShellBlockSystemPrompt = `
Shell Block Kind:

Use the "shell" kind to execute shell commands and receive the output as part of the next generation round. Use shell blocks to run tests, check build status, explore the codebase, and verify changes autonomously.

**Rules:**
- Use shell blocks to run tests, check build status, explore the codebase, or verify changes.
- The command is executed with ` + "`" + `sh -c` + "`" + ` in the project root directory.
- Both stdout and stderr are captured and returned as user content in the next round.
- A timeout of 30 seconds is enforced per command.
- Shell output is NOT available in the current response: it is returned as user content only at the start of the NEXT round, after ALL shell blocks in the response have been executed.
- You MAY emit multiple shell blocks in one response, but only when their commands are independent of one another: no shell block can use the output of another shell block from the same response.
- Do NOT emit change blocks or read blocks whose content depends on the shell output: the results have not arrived yet, so emitting them before the results arrive creates pointless loops.
- After the last shell block's closing line, emit the summary block IMMEDIATELY, then end the response and wait for the results.
- Never end a response on a shell block, and never stop at its closing line: stopping there omits the mandatory summary block, the response is treated as incomplete, and it is discarded and retried — its blocks are discarded, so the commands are never executed unless re-emitted.
- When the results arrive as user content in the next round (formatted as "Shell command: <command>" followed by the output), read them before emitting anything else. If another command is needed, emit a new shell block in that round and wait for its results in the following round.
**Security policy**: Only commands in the allowed list are executed. Allowed command categories:
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
// user content instead of being executed. Each output part ends with a
// blank line so consecutive parts in the same round stay
// paragraph-separated after verbatim part concatenation; see
// generators.TheoryOfContentUnitSeparation. The provided context allows
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
