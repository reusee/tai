package blocks

import (
	"context"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	"github.com/reusee/tai/generators"
)

func TestProcessShellBlocks(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "echo hello world"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "hello world") {
		t.Fatalf("expected output to contain 'hello world', got: %s", output)
	}
	if !strings.Contains(output, "Command succeeded") {
		t.Fatalf("expected output to contain 'Command succeeded', got: %s", output)
	}
}

func TestProcessShellBlocksSeparatesOutputsWithBlankLine(t *testing.T) {
	// Each shell output unit ends with a blank line so consecutive outputs
	// in the same round stay paragraph-separated after verbatim part
	// concatenation. See generators.TheoryOfContentUnitSeparation.
	blocks := []Block{
		{Kind: "shell", Body: "echo one"},
		{Kind: "shell", Body: "echo two"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	for i, part := range parts {
		output := string(part.(generators.Text))
		if !strings.HasSuffix(output, "\n\n") {
			t.Fatalf("shell output %d must end with a blank line, got %q", i, output)
		}
	}
}

func TestProcessShellBlocksCommandFailure(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "cat /nonexistent"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "Command failed") {
		t.Fatalf("expected output to contain 'Command failed', got: %s", output)
	}
}

func TestProcessShellBlocksEmpty(t *testing.T) {
	parts, err := ProcessShellBlocks(nil, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}

func TestProcessShellBlocksRejectsRedirection(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "echo hello > /tmp/test"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "Shell command rejected") {
		t.Fatalf("expected output to contain 'Shell command rejected', got: %s", output)
	}
	if !strings.Contains(output, "redirection") {
		t.Fatalf("expected output to mention redirection, got: %s", output)
	}
}

func TestProcessShellBlocksAllowsGitStatus(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "git status"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if strings.Contains(output, "Shell command rejected") {
		t.Fatalf("git status should be allowed, got: %s", output)
	}
}

func TestProcessShellBlocksRejectsDangerousCommand(t *testing.T) {
	// A dangerous target is rejected before execution. The target is a
	// nonexistent top-level path, so a filter regression cannot damage the
	// machine; the rejection itself is what this test guards. The classic
	// targets (rm -rf /, rm -rf *) are covered by the security package's
	// validator tests, which never execute anything.
	blocks := []Block{
		{Kind: "shell", Body: "rm -rf /nonexistent-top-level-dir"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "Shell command rejected") {
		t.Fatalf("expected output to contain 'Shell command rejected', got: %s", output)
	}
	if !strings.Contains(output, "rm") {
		t.Fatalf("expected output to mention the rejected command, got: %s", output)
	}
}

func TestProcessShellBlocksFiltersByKind(t *testing.T) {
	blocks := []Block{
		{Kind: "summary", Body: "echo hello"},
		{Kind: "shell", Body: "echo hello world"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "hello world") {
		t.Fatalf("expected output to contain 'hello world', got: %s", output)
	}
}

func TestProcessShellBlocksAllowsAnyProgram(t *testing.T) {
	// Without a configured allowlist no program list applies: kill was
	// never in the old list and still runs. `kill -l` only lists signal
	// names, so the test cannot signal any process.
	blocks := []Block{
		{Kind: "shell", Body: "kill -l"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if strings.Contains(output, "Shell command rejected") {
		t.Fatalf("kill should be allowed without an allowlist, got: %s", output)
	}
	if !strings.Contains(output, "Command succeeded") {
		t.Fatalf("expected the command to run, got: %s", output)
	}
}

func TestShellPromptsWaitForResults(t *testing.T) {
	// Shell output is returned as user content only in the NEXT round, so
	// the model must not emit content that depends on shell output in the
	// same response. Multiple independent shell blocks in one response are
	// allowed, but a shell block whose command depends on another shell
	// block's output — or a change block that depends on shell output —
	// would act on results that have not yet arrived, creating pointless
	// loops. The prompt must state the wait-for-results semantics
	// explicitly, and the stop rule must be phrased summary-first — emit
	// the summary block IMMEDIATELY after the last shell block's closing
	// line, then end the response — so no stop instruction licenses
	// halting at the closing line and omitting the summary. See
	// TheoryOfShellBlocks and TheoryOfSummaryBlocks.
	prompt := ShellBlockSystemPrompt
	if strings.Contains(prompt, "ONE shell block") {
		t.Fatal("ShellBlockSystemPrompt must not restrict the model to a single shell block per response")
	}
	if !strings.Contains(prompt, "NEXT round") {
		t.Fatal("ShellBlockSystemPrompt must state that shell output is returned only in the next round")
	}
	if !strings.Contains(prompt, "emit the summary block IMMEDIATELY") {
		t.Fatal("ShellBlockSystemPrompt must phrase the stop rule summary-first: emit the summary block immediately after the last shell block")
	}
	if strings.Contains(prompt, "stop generating") {
		t.Fatal("ShellBlockSystemPrompt must not carry a bare stop instruction before the summary requirement")
	}
	if !strings.Contains(prompt, "independent") {
		t.Fatal("ShellBlockSystemPrompt must state that multiple shell blocks are only allowed when their commands are independent")
	}
	if !strings.Contains(prompt, "Never end a response on a shell block") {
		t.Fatal("ShellBlockSystemPrompt must state the sequence rule: the block after the last shell block must be the summary block")
	}
}

func TestShellPromptSecurityPolicy(t *testing.T) {
	// The prompt teaches the destructive-pattern filter, not a program
	// allowlist: any program may run, so the prompt must not carry a
	// program list and must name the patterns that are rejected. See
	// TheoryOfShellBlocks and security.TheoryOfShellSecurity.
	prompt := ShellBlockSystemPrompt
	if !strings.Contains(prompt, "Any program may run") {
		t.Fatal("ShellBlockSystemPrompt must state that any program may run")
	}
	if !strings.Contains(prompt, "rm -rf /") {
		t.Fatal("ShellBlockSystemPrompt must name the rejected destructive patterns")
	}
	if !strings.Contains(prompt, "Output redirection") {
		t.Fatal("ShellBlockSystemPrompt must state that output redirection is rejected")
	}
	if strings.Contains(prompt, "Allowed command categories") || strings.Contains(prompt, "allowed list") {
		t.Fatal("ShellBlockSystemPrompt must not carry a program allowlist")
	}
}

func TestAllowedShellCommands(t *testing.T) {
	allowed := AllowedShellCommands{"  git status ", "", "ls", "git status"}
	if !allowed.Configured() {
		t.Fatal("a list with entries must report configured")
	}
	if !allowed.Allows("git status") {
		t.Fatal("a listed command must be allowed")
	}
	if allowed.Allows("git push") {
		t.Fatal("an unlisted command must not be allowed")
	}
	var empty AllowedShellCommands
	if empty.Configured() {
		t.Fatal("an empty list must not report configured")
	}
	if !empty.Allows("anything") {
		t.Fatal("an unconfigured list allows every command")
	}
	if empty.ShellEnabled(false) || !empty.ShellEnabled(true) {
		t.Fatal("an unconfigured list follows the shell flag")
	}
	if !allowed.ShellEnabled(false) {
		t.Fatal("a configured list enables shell blocks by itself")
	}
}

func TestAllowedShellCommandsHandleConfig(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`["git status", "ls"]`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	newDef, err := AllowedShellCommands(nil).HandleConfig("allowed_shell_commands", []*cue.Value{&v})
	if err != nil {
		t.Fatal(err)
	}
	ret, ok := newDef.(*AllowedShellCommands)
	if !ok {
		t.Fatalf("expected *AllowedShellCommands, got %T", newDef)
	}
	if !ret.Allows("git status") || !ret.Allows("ls") {
		t.Fatalf("listed commands must be allowed, got %v", *ret)
	}
	if ret.Allows("rm -rf /") {
		t.Fatal("an unlisted command must not be allowed")
	}
}

func TestProcessShellBlocksAllowlist(t *testing.T) {
	// A configured allowlist is the permission grant: a listed command
	// runs, and an unlisted one is rejected before execution with the
	// allowed commands named in the rejection. See
	// TheoryOfShellAllowlist.
	allowed := AllowedShellCommands{"echo hello"}
	blocks := []Block{
		{Kind: "shell", Body: "echo hello"},
		{Kind: "shell", Body: "echo unlisted"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background(), allowed)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	first := string(parts[0].(generators.Text))
	if !strings.Contains(first, "Command succeeded") {
		t.Fatalf("a listed command must run, got: %s", first)
	}
	second := string(parts[1].(generators.Text))
	if !strings.Contains(second, "Shell command rejected") {
		t.Fatalf("an unlisted command must be rejected, got: %s", second)
	}
	if !strings.Contains(second, "  - echo hello") {
		t.Fatalf("the rejection must name the allowed commands, got: %s", second)
	}
}

func TestShellBlockPromptAllowlist(t *testing.T) {
	if ShellBlockPrompt(nil) != ShellBlockSystemPrompt {
		t.Fatal("an unconfigured allowlist must keep the default shell prompt")
	}
	prompt := ShellBlockPrompt(AllowedShellCommands{"ls -la", "git status", "git status"})
	if !strings.Contains(prompt, "  - git status\n") {
		t.Fatalf("the prompt must list the allowed commands, got: %s", prompt)
	}
	if !strings.Contains(prompt, "  - ls -la\n") {
		t.Fatalf("the prompt must list the allowed commands, got: %s", prompt)
	}
	if strings.Contains(prompt, "Any program may run") {
		t.Fatal("the prompt must not promise that any program may run when an allowlist is configured")
	}
	if !strings.Contains(prompt, "Output redirection") {
		t.Fatal("the prompt must keep the structural rules")
	}
}

func TestAllowedShellCommandsConfigPath(t *testing.T) {
	// The config path is the contract with the closed schema. A wrong path
	// fails silently — no Config type registers the key, so nothing is ever
	// read and the feature appears to do nothing.
	paths := AllowedShellCommands(nil).ConfigPaths()
	if len(paths) != 1 || paths[0] != "allowed_shell_commands" {
		t.Fatalf("unexpected config paths: %v", paths)
	}
}
