package main

import (
	"os"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func TestUserPromptChatInputPrecedesContext(t *testing.T) {
	// The chat input must precede the parts provider content: the chat
	// arguments are prepended as the first user prompt part (ending with
	// a blank line) so the model reads the task before the long file
	// context. userPromptMockGenerator carries a positive context window
	// so the parts provider emits the file content, and counts zero
	// tokens, so the assembled user prompt stays within the restate
	// threshold and omits the verbatim system prompt restate. See
	// pipeline.TheoryOfChatBracketing and
	// components.SystemPromptRestateForUserPrompt.
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("test.md", []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return userPromptMockGenerator{}, nil
			}
		},
		func() flags.Chats { return flags.Chats{"do the next thing"} },
		func() flags.Files { return flags.Files{"test.md": true} },
	).Call(func(
		userPrompt UserPrompt,
	) {
		if len(userPrompt) < 2 {
			t.Fatalf("expected chat input and file context, got %d parts", len(userPrompt))
		}
		first, ok := userPrompt[0].(generators.Text)
		if !ok || first != "do the next thing\n\n" {
			t.Fatalf("first part must be the chat input ending with a blank line, got %#v", userPrompt[0])
		}
		second, ok := userPrompt[1].(generators.Text)
		if !ok || !strings.Contains(string(second), "# Title") {
			t.Fatalf("second part must be the file context part, got %#v", userPrompt[1])
		}
		for _, part := range userPrompt {
			if text, ok := part.(generators.Text); ok && strings.HasPrefix(string(text), "[System note:") {
				t.Fatal("a user prompt within the restate threshold must omit the verbatim system prompt restate")
			}
		}
	})
}
