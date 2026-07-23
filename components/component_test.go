package components

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

func TestComponentSetPromptSections(t *testing.T) {
	comps := ComponentSet{
		{Kind: "a", PromptSection: "prompt-a"},
		{Kind: "b", PromptSection: "prompt-b"},
		{Kind: "", PromptSection: "prompt-only"},
		{Kind: "c", PromptSection: ""},
	}
	got := comps.PromptSections()
	if got != "prompt-a\nprompt-b\nprompt-only\n" {
		t.Fatalf("got %q", got)
	}
}

func TestComponentSetRestatePrompts(t *testing.T) {
	comps := ComponentSet{
		{Kind: "a", PromptSection: "prompt-a", RestatePrompt: "restate-a"},
		{Kind: "b", PromptSection: "prompt-b"},
		{Kind: "", PromptSection: "prompt-only", RestatePrompt: "restate-only"},
		{Kind: "c", RestatePrompt: "restate-c"},
	}
	got := comps.RestatePrompts()
	if got != "restate-a\nrestate-only\nrestate-c\n" {
		t.Fatalf("got %q", got)
	}
}

func TestComponentSetUserPromptParts(t *testing.T) {
	comps := ComponentSet{
		{Kind: "a", UserPromptParts: []generators.Part{generators.Text("part-a")}},
		{Kind: "b", UserPromptParts: []generators.Part{generators.Text("part-b1"), generators.Text("part-b2")}},
		{Kind: "c"}, // no user prompt parts
	}
	parts := comps.UserPromptParts()
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if text, ok := parts[0].(generators.Text); !ok || text != "part-a" {
		t.Fatalf("expected part-a, got %v", parts[0])
	}
	if text, ok := parts[1].(generators.Text); !ok || text != "part-b1" {
		t.Fatalf("expected part-b1, got %v", parts[1])
	}
	if text, ok := parts[2].(generators.Text); !ok || text != "part-b2" {
		t.Fatalf("expected part-b2, got %v", parts[2])
	}
}

func TestComponentSetProcessable(t *testing.T) {
	comps := ComponentSet{
		{Kind: "a", Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult { return ProcessResult{} }},
		{Kind: "b", ProcessingPath: "external"},
		{Kind: "", Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult { return ProcessResult{} }},
		{Kind: "c", Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult { return ProcessResult{} }},
	}
	processable := comps.Processable()
	if len(processable) != 3 {
		t.Fatalf("expected 3 processable components, got %d", len(processable))
	}
	if processable[0].Kind != "a" || processable[1].Kind != "" || processable[2].Kind != "c" {
		t.Fatalf("unexpected kinds: %s, %s, %s", processable[0].Kind, processable[1].Kind, processable[2].Kind)
	}
}

func TestComponentSetValidate(t *testing.T) {
	t.Run("valid with process", func(t *testing.T) {
		comps := ComponentSet{
			{Kind: "a", Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult { return ProcessResult{} }},
		}
		if err := comps.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid with processing path", func(t *testing.T) {
		comps := ComponentSet{
			{Kind: "a", ProcessingPath: "external"},
		}
		if err := comps.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid with neither", func(t *testing.T) {
		comps := ComponentSet{
			{Kind: "a"},
		}
		err := comps.Validate()
		if err == nil {
			t.Fatal("expected error for component with neither Process nor ProcessingPath")
		}
		if !strings.Contains(err.Error(), "a") {
			t.Fatalf("error should mention kind 'a': %v", err)
		}
	})

	t.Run("valid prompt-only component", func(t *testing.T) {
		comps := ComponentSet{
			{PromptSection: "some prompt"},
		}
		if err := comps.Validate(); err != nil {
			t.Fatalf("prompt-only component should be valid without Process or ProcessingPath: %v", err)
		}
	})
}

func TestComponentSetProcessingCycle(t *testing.T) {
	upstream := generators.NewPrompts("system", nil)
	ps := blocks.NewParserState(upstream)

	text := ":::徕珑 <shell>\necho hello\n:::徕珑 </shell>\n" +
		":::栢彣 <continue>\nnext round\n:::栢彣 </continue>\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*blocks.ParserState)

	comps := ComponentSet{
		{
			Kind: "shell",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, newPs, err := blocks.ProcessShellBlocks(pctx.ParserState)
				return ProcessResult{ParserState: newPs, Parts: parts, Err: err}
			},
		},
		{
			Kind: "continue",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, newPs := blocks.ProcessContinueBlocks(pctx.ParserState)
				return ProcessResult{ParserState: newPs, Parts: parts}
			},
		},
	}

	var combinedParts []generators.Part
	continueRound := false
	currentPs := ps
	for _, comp := range comps.Processable() {
		result := comp.Process(context.Background(), &ProcessContext{
			ParserState: currentPs,
		})
		if result.Err != nil {
			t.Fatalf("unexpected error from %s: %v", comp.Kind, result.Err)
		}
		if result.ParserState != nil {
			currentPs = result.ParserState
		}
		combinedParts = append(combinedParts, result.Parts...)
		if result.Continue {
			continueRound = true
			break
		}
	}

	if continueRound {
		t.Fatal("expected no continue round")
	}
	if len(combinedParts) != 2 {
		t.Fatalf("expected 2 combined parts, got %d", len(combinedParts))
	}
	shellOutput := string(combinedParts[0].(generators.Text))
	if !strings.Contains(shellOutput, "hello") {
		t.Fatalf("shell output missing 'hello': %s", shellOutput)
	}
	continueOutput := string(combinedParts[1].(generators.Text))
	if !strings.Contains(continueOutput, "next round") {
		t.Fatalf("continue body missing 'next round': %s", continueOutput)
	}
}

func TestComponentSetContinueStopsProcessing(t *testing.T) {
	called := make(map[string]bool)
	comps := ComponentSet{
		{
			Kind: "first",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				called["first"] = true
				return ProcessResult{Continue: true, State: pctx.State}
			},
		},
		{
			Kind: "second",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				called["second"] = true
				return ProcessResult{}
			},
		},
	}

	continueRound := false
	for _, comp := range comps.Processable() {
		result := comp.Process(context.Background(), &ProcessContext{})
		if result.Continue {
			continueRound = true
			break
		}
	}

	if !continueRound {
		t.Fatal("expected continue round")
	}
	if !called["first"] {
		t.Fatal("first component should have been called")
	}
	if called["second"] {
		t.Fatal("second component should NOT have been called after continue")
	}
}

func TestProcessResultErrorPropagation(t *testing.T) {
	testErr := errors.New("test error")
	comps := ComponentSet{
		{
			Kind: "failing",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				return ProcessResult{Err: testErr}
			},
		},
	}

	for _, comp := range comps.Processable() {
		result := comp.Process(context.Background(), &ProcessContext{})
		if result.Err != testErr {
			t.Fatalf("expected test error, got %v", result.Err)
		}
	}
}

func TestCommonComponents(t *testing.T) {
	t.Run("with shell", func(t *testing.T) {
		comps := CommonComponents(true)
		processable := comps.Processable()
		if len(processable) != 2 {
			t.Fatalf("expected 2 processable components (shell, continue), got %d", len(processable))
		}
		if processable[0].Kind != "shell" {
			t.Fatalf("expected first component to be shell, got %s", processable[0].Kind)
		}
		if processable[1].Kind != "continue" {
			t.Fatalf("expected second component to be continue, got %s", processable[1].Kind)
		}
		prompt := comps.PromptSections()
		if !strings.Contains(prompt, "Shell Block Kind") {
			t.Fatal("PromptSections should contain shell block prompt")
		}
		if !strings.Contains(prompt, "Continue Block Kind") {
			t.Fatal("PromptSections should contain continue block prompt")
		}
		if err := comps.Validate(); err != nil {
			t.Fatalf("Validate failed: %v", err)
		}
	})

	t.Run("without shell", func(t *testing.T) {
		comps := CommonComponents(false)
		processable := comps.Processable()
		if len(processable) != 1 {
			t.Fatalf("expected 1 processable component (continue), got %d", len(processable))
		}
		if processable[0].Kind != "continue" {
			t.Fatalf("expected component to be continue, got %s", processable[0].Kind)
		}
		prompt := comps.PromptSections()
		if strings.Contains(prompt, "Shell Block Kind") {
			t.Fatal("PromptSections should not contain shell block prompt when shell is disabled")
		}
		if !strings.Contains(prompt, "Continue Block Kind") {
			t.Fatal("PromptSections should contain continue block prompt")
		}
		if err := comps.Validate(); err != nil {
			t.Fatalf("Validate failed: %v", err)
		}
	})
}
