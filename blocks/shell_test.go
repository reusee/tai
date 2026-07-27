package blocks

import (
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestProcessShellBlocks(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "echo hello world"},
	}
	parts, err := ProcessShellBlocks(blocks)
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

func TestProcessShellBlocksCommandFailure(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "cat /nonexistent"},
	}
	parts, err := ProcessShellBlocks(blocks)
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
	parts, err := ProcessShellBlocks(nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}

func TestValidateShellCommand(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantErr bool
	}{
		// Allowed commands
		{"ls", "ls -la", false},
		{"cat", "cat file.txt", false},
		{"grep", "grep -r pattern .", false},
		{"go test", "go test ./...", false},
		{"go build", "go build ./...", false},
		{"git status", "git status", false},
		{"git log", "git log --oneline", false},
		{"echo", "echo hello", false},
		{"pwd", "pwd", false},
		{"find", "find . -name *.go", false},
		{"pipe", "ls -la | grep go", false},
		{"semicolon", "echo hello; echo world", false},
		{"and", "go test ./... && echo done", false},
		{"absolute path", "/usr/bin/ls -la", false},
		{"npm list", "npm list", false},
		{"node version", "node --version", false},

		// Forbidden commands
		{"rm", "rm -rf /", true},
		{"kill", "kill -9 1234", true},
		{"mv", "mv a b", true},
		{"cp", "cp a b", true},
		{"chmod", "chmod 777 file", true},
		{"chown", "chown user file", true},
		{"shutdown", "shutdown -h now", true},
		{"dd", "dd if=/dev/zero of=file", true},
		{"sed", "sed -i 's/a/b/g' file", true},
		{"awk", "awk '{print $1}' file", true},

		// Output redirection
		{"redirect", "echo hello > file.txt", true},
		{"append", "echo hello >> file.txt", true},

		// find -exec
		{"find exec", "find . -exec rm {} \\;", true},
		{"find execdir", "find . -execdir rm {}", true},

		// Git write operations
		{"git commit", "git commit -m msg", true},
		{"git push", "git push origin main", true},
		{"git pull", "git pull", true},
		{"git merge", "git merge feature", true},
		{"git checkout", "git checkout main", true},
		{"git add", "git add .", true},
		{"git branch", "git branch -D feature", true},
		{"git config", "git config user.name foo", true},
		{"git stash", "git stash", true},

		// Go modifying operations
		{"go fmt", "go fmt ./...", true},
		{"go mod", "go mod tidy", true},
		{"go install", "go install", true},
		{"go run", "go run main.go", true},
		{"go get", "go get example.com/pkg", true},

		// Empty command
		{"empty", "", true},
		{"whitespace", "   ", true},

		// Pipe with forbidden command
		{"pipe with rm", "ls | rm", true},
		{"and with kill", "echo hi && kill 1234", true},

		// Command without required subcommand
		{"git no subcommand", "git", true},
		{"go no subcommand", "go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShellCommand(tt.cmd)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for command %q, got nil", tt.cmd)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for command %q: %v", tt.cmd, err)
			}
		})
	}
}

func TestProcessShellBlocksRejectsForbiddenCommand(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "rm -rf /tmp/test"},
	}
	parts, err := ProcessShellBlocks(blocks)
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

func TestProcessShellBlocksRejectsRedirection(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "echo hello > /tmp/test"},
	}
	parts, err := ProcessShellBlocks(blocks)
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
	parts, err := ProcessShellBlocks(blocks)
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
