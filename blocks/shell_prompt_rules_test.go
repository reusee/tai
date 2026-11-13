package blocks

import (
	"strings"
	"testing"

	"github.com/reusee/tai/security"
)

// TestShellPromptCoversValidatorRules guards the parity between the shell
// prompt and the validator: every representative command the validator
// rejects must have a rule in the prompt that names the reason, so the model
// does not discover a restriction by a rejection round. See
// TheoryOfShellBlocks and security.TheoryOfShellSecurity.
func TestShellPromptCoversValidatorRules(t *testing.T) {
	cases := []struct {
		command string
		rule    string
	}{
		{"echo 'unclosed", "must be valid sh syntax"},
		{"ls > out.txt", "Output redirection is rejected in every form"},
		{"ls <> out.txt", "Output redirection is rejected in every form"},
		{"ls 2>&1", "2>&1"},
		{"if true; then ls; fi", "compound commands"},
		{"while true; do ls; done", "compound commands"},
		{"until true; do ls; done", "until"},
		{"select x in a; do ls; done", "select"},
		{"(ls)", "subshells"},
		{"{ ls; }", "blocks"},
		{"f() { ls; }", "function definitions"},
		{"(( 1 + 1 ))", "arithmetic commands"},
		{"[[ -f file ]]", "test clauses"},
		{"time ls", "time and let clauses"},
		{"let x=1", "time and let clauses"},
		{"declare x=1", "declaration builtins"},
		{"export FOO=bar", "declaration builtins"},
		{"coproc ls", "the coproc clause"},
		{"FOO=bar", "bare variable assignment"},
		{"", "must not be empty"},
		{"ls &", "Background execution"},
		{"rm -rf /nonexistent-top-level-dir", "top-level system directory"},
		{"echo $(rm -rf /nonexistent-top-level-dir)", "command substitution"},
	}
	prompts := []string{
		ShellBlockSystemPrompt,
		ShellBlockPrompt(AllowedShellCommands{"ls -la"}),
	}
	for _, prompt := range prompts {
		for _, c := range cases {
			if err := security.ValidateShellCommand(c.command); err == nil {
				t.Fatalf("the validator must reject %q", c.command)
			}
			if !strings.Contains(prompt, c.rule) {
				t.Fatalf("the prompt must state the rule %q for command %q", c.rule, c.command)
			}
		}
	}
	// Every output redirection operator the validator rejects must be named
	// in the structural rules, so the model reads the whole set at once.
	for _, op := range []string{">", ">>", "<>", ">&", ">|", ">>|", "&>", "&>>", "&>|", "&>>|"} {
		if !strings.Contains(shellStructuralRules, op) {
			t.Fatalf("the structural rules must name the rejected redirection %q", op)
		}
	}
	// The connectors the validator accepts must be named as allowed.
	for _, connector := range []string{"|", "|&", "&&", "||"} {
		if !strings.Contains(shellStructuralRules, connector) {
			t.Fatalf("the structural rules must name the allowed connector %q", connector)
		}
	}
}
