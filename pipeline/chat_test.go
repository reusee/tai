package pipeline

import (
	"context"
	"io"
	"testing"

	"github.com/reusee/tai/generators"
)

// TestBuildChatIdleUsesChatInput pins the ChatInput seam: the idle
// handler must read lines exclusively through the injected reader and
// never construct a terminal reader itself, so a TUI session can route
// the prompt through its own input channel. See TheoryOfChatInput.
func TestBuildChatIdleUsesChatInput(t *testing.T) {
	newState := func() generators.State {
		return generators.NewPrompts("", nil)
	}

	t.Run("submits input as user content", func(t *testing.T) {
		var prompts []string
		chatInput := ChatInput(func(prompt string) (string, error) {
			prompts = append(prompts, prompt)
			return "hello", nil
		})
		// The builder is called with a nil generator: none of the tested
		// paths touches it (only /tap reads generator.Spec()).
		idle := Module{}.BuildChatIdle(chatInput, nil)(nil, nil)
		state, cont, err := idle(context.Background(), newState())
		if err != nil {
			t.Fatal(err)
		}
		if !cont {
			t.Fatal("want the loop to continue after input")
		}
		if len(prompts) != 1 || prompts[0] != ">> " {
			t.Fatalf("prompts %v, want one \">> \"", prompts)
		}
		var last *generators.Content
		for c := range state.Contents() {
			last = c
		}
		if last == nil || last.Role != generators.RoleUser || len(last.Parts) != 1 {
			t.Fatalf("user content not appended: %+v", last)
		}
		if text, ok := last.Parts[0].(generators.Text); !ok || string(text) != "hello\n\n" {
			t.Fatalf("got part %v, want \"hello\\n\\n\"", last.Parts[0])
		}
	})

	t.Run("EOF ends the session", func(t *testing.T) {
		idle := Module{}.BuildChatIdle(ChatInput(func(string) (string, error) {
			return "", io.EOF
		}), nil)(nil, nil)
		_, cont, err := idle(context.Background(), newState())
		if err != nil {
			t.Fatal(err)
		}
		if cont {
			t.Fatal("want the loop to stop on EOF")
		}
	})

	t.Run("quit command ends the session", func(t *testing.T) {
		idle := Module{}.BuildChatIdle(ChatInput(func(string) (string, error) {
			return "/quit", nil
		}), nil)(nil, nil)
		_, cont, err := idle(context.Background(), newState())
		if err != nil {
			t.Fatal(err)
		}
		if cont {
			t.Fatal("want the loop to stop on /quit")
		}
	})

	t.Run("blank input prompts again", func(t *testing.T) {
		calls := 0
		idle := Module{}.BuildChatIdle(ChatInput(func(string) (string, error) {
			calls++
			if calls == 1 {
				return "   ", nil
			}
			return "/quit", nil
		}), nil)(nil, nil)
		_, cont, err := idle(context.Background(), newState())
		if err != nil {
			t.Fatal(err)
		}
		if cont {
			t.Fatal("want the loop to stop on /quit")
		}
		if calls != 2 {
			t.Fatalf("ChatInput called %d times, want 2", calls)
		}
	})
}
