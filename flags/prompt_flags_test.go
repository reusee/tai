package flags

import (
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
