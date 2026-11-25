package flags

import (
	"strings"
	"testing"

	"github.com/reusee/dscope"
)

func TestParsePromptFlags(t *testing.T) {
	for _, tt := range []struct {
		name   string
		key    string
		prompt string
	}{
		{"align", "align", alignPrompt},
		{"clean", "clean", cleanPrompt},
		{"distill", "distill", distillPrompt},
		{"fix", "fix", fixPrompt},
	} {
		t.Run(tt.name+"/solo", func(t *testing.T) {
			scope := dscope.New(Module{})
			result, err := Parse(scope, []string{tt.key})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			chats := result.Get[Chats]()
			if len(chats) != 1 || chats[0] != tt.prompt {
				t.Fatalf("expected [%s], got %v", tt.prompt, chats)
			}
		})
		t.Run(tt.name+"/composed", func(t *testing.T) {
			scope := dscope.New(Module{})
			result, err := Parse(scope, []string{"chat", "hello", tt.key})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			chats := result.Get[Chats]()
			if len(chats) != 2 || chats[0] != "hello" || chats[1] != tt.prompt {
				t.Fatalf("expected [hello %s], got %v", tt.prompt, chats)
			}
		})
	}
}

func TestDistillPromptReadsExistingTheoryFirst(t *testing.T) {
	for _, want := range []string{"_theory.md", "ingest block", "basis", "Write"} {
		if !strings.Contains(distillPrompt, want) {
			t.Fatalf("distill prompt misses %q: %s", want, distillPrompt)
		}
	}
	if strings.Contains(distillPrompt, "_theory.go") {
		t.Fatalf("distill prompt still names the Go theory file: %s", distillPrompt)
	}
}

func TestDistillPromptDistillsFromImplementation(t *testing.T) {
	for _, want := range []string{"implementation", "comments", "no theory text"} {
		if !strings.Contains(distillPrompt, want) {
			t.Fatalf("distill prompt misses %q: %s", want, distillPrompt)
		}
	}
}
