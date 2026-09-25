package blocks

import (
	"fmt"
	"slices"
	"strings"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// TheoryOfShellAllowlist documents the shell command allowlist: its
// permission semantics, its exact matching rule, and its normalization.
const TheoryOfShellAllowlist = `
The shell allowlist lets the user decide which commands the model may
run. A configured list is the permission grant and is authoritative:
shell block processing is enabled when the shell flag is set or the list
is configured, and with a configured list only the listed commands run.
An empty list keeps the previous behavior, so an unconfigured session is
unchanged.

An entry matches a command when the two are equal after trimming
surrounding whitespace. Exact matching keeps the list safe to reason
about: a rule language — prefix, glob, argument constraints — matches
commands the user did not intend.

The list narrows what may run and never widens it: the structural
validator still runs on every listed command. Entries are trimmed,
deduplicated, and sorted for both matching and prompt rendering, so
equal configurations produce byte-identical prompts and the LLM prefix
cache survives.
`

var _ configs.Config = AllowedShellCommands(nil)

// AllowedShellCommands lists the shell commands the model may execute,
// read from the allowed_shell_commands config path. A configured list is
// the user's own decision to let the model run the listed commands, so it
// enables shell block processing by itself; an empty list leaves the
// shell flag in charge. See TheoryOfShellAllowlist.
type AllowedShellCommands []string

func (a AllowedShellCommands) ConfigPaths() []string {
	return []string{"allowed_shell_commands"}
}

func (a AllowedShellCommands) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret, err := configs.AppendStringsConfig(a, values)
	if err != nil {
		return nil, err
	}
	v := AllowedShellCommands(ret)
	return &v, nil
}

// cleaned returns the normalized list: entries trimmed, empty entries
// dropped, duplicates removed, and the result sorted. Both matching and
// prompt rendering consume the normalized form, so equal configurations
// behave and render identically. See TheoryOfShellAllowlist.
func (a AllowedShellCommands) cleaned() []string {
	seen := make(map[string]bool, len(a))
	var ret []string
	for _, cmd := range a {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" || seen[cmd] {
			continue
		}
		seen[cmd] = true
		ret = append(ret, cmd)
	}
	slices.Sort(ret)
	return ret
}

// Configured reports whether the list carries at least one usable entry.
func (a AllowedShellCommands) Configured() bool {
	return len(a.cleaned()) > 0
}

// Allows reports whether a command may run: an unconfigured list allows
// every command, and a configured list allows exactly the listed commands.
func (a AllowedShellCommands) Allows(cmd string) bool {
	cleaned := a.cleaned()
	if len(cleaned) == 0 {
		return true
	}
	return slices.Contains(cleaned, strings.TrimSpace(cmd))
}

// ShellEnabled reports whether shell block processing is enabled: the
// shell flag turns it on, and a configured allowlist turns it on by
// itself, because the user who lists commands has already decided to let
// the model run them.
func (a AllowedShellCommands) ShellEnabled(shell bool) bool {
	return shell || a.Configured()
}

// rejectionText renders the user content returned when the allowlist
// rejects a command: the command, the reason, and every command that may
// run, so the model corrects itself without a discovery round. It ends
// with a blank line so consecutive output units stay paragraph-separated;
// see generators.TheoryOfContentUnitSeparation.
func (a AllowedShellCommands) rejectionText(cmd string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb,
		"Shell command rejected: %s\n\nError: the command is not in the configured command allowlist. Allowed commands:\n",
		cmd,
	)
	for _, allowed := range a.cleaned() {
		sb.WriteString("  - ")
		sb.WriteString(allowed)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// AllowedShellCommands provides the default: no allowlist, so the shell
// flag alone decides and any command passing the structural filter runs.
// configs.Load forks the configured value over it. See
// TheoryOfShellAllowlist.
func (Module) AllowedShellCommands() AllowedShellCommands {
	return nil
}

const shellPromptHead = `
Shell Block Kind:

Use the "shell" kind to execute shell commands and receive the output as part of the next generation round. Use shell blocks to run tests, check build status, explore the codebase, and verify changes autonomously.

**Rules:**
- Use shell blocks to run tests, check build status, explore the codebase, or verify changes.
- The command is executed with ` + "`" + `sh -c` + "`" + ` in the project root directory.
- Both stdout and stderr are captured and returned as user content in the next round.
- A timeout of 30 seconds is enforced per command.
- Shell output is NOT available in the current response: it is returned as user content only at the start of the NEXT round, after ALL shell blocks in the response have been executed.
- You MAY emit multiple shell blocks in one response, but only when their commands are independent of one another: no shell block can use the output of another shell block from the same response.
- Do NOT emit change blocks or ingest blocks whose content depends on the shell output: the results have not arrived yet, so emitting them before the results arrive creates pointless loops.
- After the last shell block's closing line, emit the summary block IMMEDIATELY, then end the response and wait for the results.
- Never end a response on a shell block, and never stop at its closing line: stopping there omits the mandatory summary block, the response is treated as incomplete, and it is discarded and retried — its blocks are discarded, so the commands are never executed unless re-emitted.
- When the results arrive as user content in the next round (formatted as "Shell command: <command>" followed by the output), read them before emitting anything else. If another command is needed, emit a new shell block in that round and wait for its results in the following round.
`

const shellStructuralRules = `  - Output redirection (>, >>) is not allowed.
  - Deleting the filesystem root, a top-level system directory (e.g. /usr, /etc, /home), the whole current directory, or the home directory is rejected, e.g. rm -rf /, rm -rf /*, rm -rf *, rm -rf ~, rm -rf $HOME. Delete named files or directories instead.
  - Background execution (&) and coprocesses are rejected: their output cannot be captured.
`

const shellAnyProgramPolicy = "**Security policy**: Any program may run. Only common destructive patterns are rejected:\n" + shellStructuralRules

const shellPromptTail = `- If a command is rejected, the error message will be returned as user content. Adjust the command and try again.
- Shell output triggers a new generation round so the model can act on the results.
`

// ShellBlockPrompt returns the shell block system prompt for the given
// allowlist: an unconfigured list yields the default prompt, and a
// configured list yields the allowlist policy, which names every command
// the model may run and keeps the structural rules. The prompt teaches
// the policy the executor enforces, so the model neither discovers the
// list by rejection rounds nor emits blocked commands from habit. See
// TheoryOfShellAllowlist.
func ShellBlockPrompt(allowed AllowedShellCommands) string {
	if !allowed.Configured() {
		return ShellBlockSystemPrompt
	}
	return shellPromptHead + shellAllowedPolicy(allowed) + shellPromptTail
}

// shellAllowedPolicy renders the security-policy section of a session
// with a configured allowlist: the listed commands, and the structural
// rules that still apply to every one of them.
func shellAllowedPolicy(allowed AllowedShellCommands) string {
	var sb strings.Builder
	sb.WriteString("**Security policy**: The user configured a command allowlist. Only these commands may run, each of them matched as a whole:\n")
	for _, cmd := range allowed.cleaned() {
		sb.WriteString("  - ")
		sb.WriteString(cmd)
		sb.WriteString("\n")
	}
	sb.WriteString("Every other command is rejected before execution. The structural rules still apply to every listed command:\n")
	sb.WriteString(shellStructuralRules)
	return sb.String()
}
