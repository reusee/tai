package security

import (
	"testing"
)

func TestValidateShellCommand(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantErr bool
	}{
		// Any program may run: the allowlist is gone.
		{"ls", "ls -la", false},
		{"cat", "cat file.txt", false},
		{"grep", "grep -r pattern .", false},
		{"sed", "sed -i 's/a/b/g' file", false},
		{"awk", "awk '{print $1}' file", false},
		{"cp", "cp a b", false},
		{"mv", "mv a b", false},
		{"chmod", "chmod 777 file", false},
		{"chown", "chown user file", false},
		{"dd", "dd if=/dev/zero of=file", false},
		{"kill", "kill -9 1234", false},
		{"python -c", "python -c 'print(1)'", false},
		{"node -e", "node -e 'console.log(1)'", false},
		{"find exec", "find . -exec rm {} \\;", false},
		{"env command", "env rm -rf /tmp/work", false},
		{"cargo run", "cargo run", false},
		{"java -jar", "java -jar file.jar", false},
		{"git commit", "git commit -m msg", false},
		{"git push", "git push origin main", false},
		{"bare git", "git", false},
		{"go mod", "go mod tidy", false},
		{"go run", "go run main.go", false},
		{"bare go", "go", false},
		{"cd", "cd /tmp && ls", false},
		{"absolute path", "/usr/bin/ls -la", false},
		{"pipe", "ls -la | grep go", false},
		{"and", "go test ./... && echo done", false},

		// Destructive rm targets are rejected.
		{"rm root", "rm -rf /", true},
		{"rm root glob", "rm -rf /*", true},
		{"rm all", "rm -rf *", true},
		{"rm current dir", "rm -rf .", true},
		{"rm current dir glob", "rm -rf ./*", true},
		{"rm parent dir", "rm -rf ..", true},
		{"rm parent glob", "rm -rf ../*", true},
		{"rm home tilde", "rm -rf ~", true},
		{"rm home tilde glob", "rm -rf ~/*", true},
		{"rm home variable", "rm -rf $HOME", true},
		{"rm home braced", "rm -rf ${HOME}", true},
		{"rm home variable glob", "rm -rf $HOME/*", true},
		{"rm top level dir", "rm -rf /usr", true},
		{"rm top level dir glob", "rm -rf /etc/*", true},
		{"rm resolved top level", "rm -rf /tmp/../etc", true},

		// Named targets pass.
		{"rm named dir", "rm -rf build", false},
		{"rm relative path", "rm -rf ./build/cache", false},
		{"rm absolute named path", "rm -rf /tmp/work", false},
		{"rm file", "rm -f file.txt", false},
		{"rm dynamic target", "rm -rf $GOCACHE", false},

		// Output redirection.
		{"redirect", "echo hello > file.txt", true},
		{"append", "echo hello >> file.txt", true},
		{"input redirect ok", "cat < file.txt", false},
		{"quoted redirection", "echo \"a > b\"", false},

		// Empty command.
		{"empty", "", true},
		{"whitespace", "   ", true},

		// Background execution.
		{"background", "ls &", true},

		// Nested commands are recursively validated.
		{"cmd subst allowed", "echo $(whoami)", false},
		{"cmd subst dangerous", "echo $(rm -rf /)", true},
		{"pipe with dangerous rm", "ls | rm -rf /", true},
		{"and with dangerous rm", "echo hi && rm -rf *", true},
		{"proc subst allowed", "diff <(ls) <(ls)", false},
		{"proc subst dangerous", "diff <(rm -rf /) <(ls)", true},
		{"heredoc cmd subst", "cat <<EOF\n$(rm -rf /)\nEOF", true},
		{"arithm cmd subst", "echo $(( $(rm -rf /) ))", true},
		{"param exp cmd subst", "echo ${var/$(rm -rf /)/x}", true},
		{"brace exp cmd subst", "echo {a,$(rm -rf /)}", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateShellCommand(tt.cmd)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for command %q, got nil", tt.cmd)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for command %q: %v", tt.cmd, err)
			}
		})
	}
}
