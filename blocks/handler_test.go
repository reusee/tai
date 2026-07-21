package blocks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestBlockBindingsPromptSections(t *testing.T) {
	bindings := BlockBindings{
		{Kind: "a", PromptSection: "prompt-a"},
		{Kind: "b", PromptSection: "prompt-b"},
		{Kind: "c", PromptSection: ""},
	}
	got := bindings.PromptSections()
	if got != "prompt-a\nprompt-b\n" {
		t.Fatalf("got %q", got)
	}
}

func TestBlockBindingsProcessable(t *testing.T) {
	bindings := BlockBindings{
		{Kind: "a", Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult { return ProcessResult{} }},
		{Kind: "b", ProcessingPath: "external"},
		{Kind: "c", Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult { return ProcessResult{} }},
	}
	processable := bindings.Processable()
	if len(processable) != 2 {
		t.Fatalf("expected 2 processable bindings, got %d", len(processable))
	}
	if processable[0].Kind != "a" || processable[1].Kind != "c" {
		t.Fatalf("unexpected kinds: %s, %s", processable[0].Kind, processable[1].Kind)
	}
}

func TestBlockBindingsValidate(t *testing.T) {
	t.Run("valid with process", func(t *testing.T) {
		bindings := BlockBindings{
			{Kind: "a", Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult { return ProcessResult{} }},
		}
		if err := bindings.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid with processing path", func(t *testing.T) {
		bindings := BlockBindings{
			{Kind: "a", ProcessingPath: "external"},
		}
		if err := bindings.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid with neither", func(t *testing.T) {
		bindings := BlockBindings{
			{Kind: "a"},
		}
		err := bindings.Validate()
		if err == nil {
			t.Fatal("expected error for binding with neither Process nor ProcessingPath")
		}
		if !strings.Contains(err.Error(), "a") {
			t.Fatalf("error should mention kind 'a': %v", err)
		}
	})
}

func TestBlockBindingsProcessingCycle(t *testing.T) {
	// Simulate a full processing cycle with two bindings:
	// - "shell" produces parts and does not continue
	// - "continue" produces parts and does not continue
	// Combined parts should trigger a new round.
	upstream := &mockState{systemPrompt: "system"}
	ps := NewParserState(upstream)

	text := ":::徕珑 <shell>\necho hello\n:::徕珑 </shell>\n" +
		":::栢彣 <continue>\nnext round\n:::栢彣 </continue>\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	bindings := BlockBindings{
		{
			Kind: "shell",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, newPs, err := ProcessShellBlocks(pctx.ParserState)
				return ProcessResult{ParserState: newPs, Parts: parts, Err: err}
			},
		},
		{
			Kind: "continue",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, newPs := ProcessContinueBlocks(pctx.ParserState)
				return ProcessResult{ParserState: newPs, Parts: parts}
			},
		},
	}

	var combinedParts []generators.Part
	continueRound := false
	currentPs := ps
	for _, binding := range bindings.Processable() {
		result := binding.Process(context.Background(), &ProcessContext{
			ParserState: currentPs,
		})
		if result.Err != nil {
			t.Fatalf("unexpected error from %s: %v", binding.Kind, result.Err)
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
	// First part should contain shell output
	shellOutput := string(combinedParts[0].(generators.Text))
	if !strings.Contains(shellOutput, "hello") {
		t.Fatalf("shell output missing 'hello': %s", shellOutput)
	}
	// Second part should contain continue body
	continueOutput := string(combinedParts[1].(generators.Text))
	if !strings.Contains(continueOutput, "next round") {
		t.Fatalf("continue body missing 'next round': %s", continueOutput)
	}
}

func TestBlockBindingsContinueStopsProcessing(t *testing.T) {
	// When a binding returns Continue=true, subsequent bindings should
	// not be processed. This matches the behavior where request-context
	// triggers a new round before shell/continue are processed.
	called := make(map[string]bool)
	bindings := BlockBindings{
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
	for _, binding := range bindings.Processable() {
		result := binding.Process(context.Background(), &ProcessContext{})
		if result.Continue {
			continueRound = true
			break
		}
	}

	if !continueRound {
		t.Fatal("expected continue round")
	}
	if !called["first"] {
		t.Fatal("first binding should have been called")
	}
	if called["second"] {
		t.Fatal("second binding should NOT have been called after continue")
	}
}

func TestProcessResultErrorPropagation(t *testing.T) {
	testErr := errors.New("test error")
	bindings := BlockBindings{
		{
			Kind: "failing",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				return ProcessResult{Err: testErr}
			},
		},
	}

	for _, binding := range bindings.Processable() {
		result := binding.Process(context.Background(), &ProcessContext{})
		if result.Err != testErr {
			t.Fatalf("expected test error, got %v", result.Err)
		}
	}
}

func TestCommonBlockBindings(t *testing.T) {
	t.Run("with shell", func(t *testing.T) {
		bindings := CommonBlockBindings(true)
		processable := bindings.Processable()
		if len(processable) != 2 {
			t.Fatalf("expected 2 processable bindings (shell, continue), got %d", len(processable))
		}
		if processable[0].Kind != "shell" {
			t.Fatalf("expected first binding to be shell, got %s", processable[0].Kind)
		}
		if processable[1].Kind != "continue" {
			t.Fatalf("expected second binding to be continue, got %s", processable[1].Kind)
		}
		prompt := bindings.PromptSections()
		if !strings.Contains(prompt, "Shell Block Kind") {
			t.Fatal("PromptSections should contain shell block prompt")
		}
		if !strings.Contains(prompt, "Continue Block Kind") {
			t.Fatal("PromptSections should contain continue block prompt")
		}
		if err := bindings.Validate(); err != nil {
			t.Fatalf("Validate failed: %v", err)
		}
	})

	t.Run("without shell", func(t *testing.T) {
		bindings := CommonBlockBindings(false)
		processable := bindings.Processable()
		if len(processable) != 1 {
			t.Fatalf("expected 1 processable binding (continue), got %d", len(processable))
		}
		if processable[0].Kind != "continue" {
			t.Fatalf("expected binding to be continue, got %s", processable[0].Kind)
		}
		prompt := bindings.PromptSections()
		if strings.Contains(prompt, "Shell Block Kind") {
			t.Fatal("PromptSections should not contain shell block prompt when shell is disabled")
		}
		if !strings.Contains(prompt, "Continue Block Kind") {
			t.Fatal("PromptSections should contain continue block prompt")
		}
		if err := bindings.Validate(); err != nil {
			t.Fatalf("Validate failed: %v", err)
		}
	})
}
