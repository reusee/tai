package components

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
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

func TestComponentSetAllProcessableCalled(t *testing.T) {
	called := make(map[string]bool)
	comps := ComponentSet{
		{
			Kind: "first",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				called["first"] = true
				return ProcessResult{Parts: []generators.Part{generators.Text("first")}}
			},
		},
		{
			Kind: "second",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				called["second"] = true
				return ProcessResult{Parts: []generators.Part{generators.Text("second")}}
			},
		},
	}

	var combinedParts []generators.Part
	for _, comp := range comps.Processable() {
		result := comp.Process(context.Background(), &ProcessContext{})
		combinedParts = append(combinedParts, result.Parts...)
	}

	if !called["first"] {
		t.Fatal("first component should have been called")
	}
	if !called["second"] {
		t.Fatal("second component should have been called")
	}
	if len(combinedParts) != 2 {
		t.Fatalf("expected 2 combined parts, got %d", len(combinedParts))
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
	})
}

func TestProcessComponents(t *testing.T) {
	t.Run("accumulates parts from multiple components", func(t *testing.T) {
		allBlocks := []blocks.Block{
			{Kind: "shell", Body: "echo hello"},
			{Kind: "continue", Body: "next round"},
		}

		comps := ComponentSet{
			{
				Kind: "shell",
				Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
					parts := []generators.Part{generators.Text("shell output: " + pctx.Blocks[0].Body)}
					return ProcessResult{Parts: parts}
				},
			},
			{
				Kind: "continue",
				Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
					parts := []generators.Part{generators.Text(pctx.Blocks[0].Body)}
					return ProcessResult{Parts: parts}
				},
			},
		}

		remaining, _, combinedParts, triggered, err := ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, nil, false,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !triggered {
			t.Fatal("expected triggered=true")
		}
		if len(combinedParts) != 2 {
			t.Fatalf("expected 2 parts, got %d", len(combinedParts))
		}
		shellOutput := string(combinedParts[0].(generators.Text))
		if !strings.Contains(shellOutput, "hello") {
			t.Fatalf("shell output missing 'hello': %s", shellOutput)
		}
		continueOutput := string(combinedParts[1].(generators.Text))
		if !strings.Contains(continueOutput, "next round") {
			t.Fatalf("continue body missing 'next round': %s", continueOutput)
		}
		if len(remaining) != 0 {
			t.Fatalf("expected 0 remaining blocks, got %d", len(remaining))
		}
	})

	t.Run("returns error from component", func(t *testing.T) {
		testErr := errors.New("component error")
		comps := ComponentSet{
			{
				Kind: "failing",
				Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
					return ProcessResult{Err: testErr}
				},
			},
		}

		allBlocks := []blocks.Block{
			{Kind: "failing", Body: "test"},
		}

		_, _, _, _, err := ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, nil, false,
		)
		if err != testErr {
			t.Fatalf("expected testErr, got %v", err)
		}
	})

	t.Run("enforces max rounds", func(t *testing.T) {
		comps := ComponentSet{
			{
				Kind:      "repeating",
				MaxRounds: 2,
				Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
					return ProcessResult{Parts: []generators.Part{generators.Text("part")}}
				},
			},
		}

		allBlocks := []blocks.Block{
			{Kind: "repeating", Body: "test"},
		}

		roundCounts := make(map[string]int)
		_, _, _, _, err := ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, roundCounts, true,
		)
		if err != nil {
			t.Fatalf("first call should succeed, got: %v", err)
		}
		if roundCounts["repeating"] != 1 {
			t.Fatalf("expected roundCounts 1, got %d", roundCounts["repeating"])
		}

		// Second call: roundCounts["repeating"] = 2, which is == MaxRounds, so OK
		_, _, _, _, err = ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, roundCounts, true,
		)
		if err != nil {
			t.Fatalf("second call should succeed (count==MaxRounds), got: %v", err)
		}

		// Third call: roundCounts["repeating"] = 3, which is > MaxRounds, so error
		_, _, _, _, err = ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, roundCounts, true,
		)
		if err == nil {
			t.Fatal("expected max rounds exceeded error on third call")
		}
		if !strings.Contains(err.Error(), "max repeating rounds (2) exceeded") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("does not enforce max rounds when disabled", func(t *testing.T) {
		comps := ComponentSet{
			{
				Kind:      "repeating",
				MaxRounds: 1,
				Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
					return ProcessResult{Parts: []generators.Part{generators.Text("part")}}
				},
			},
		}

		allBlocks := []blocks.Block{
			{Kind: "repeating", Body: "test"},
		}

		roundCounts := make(map[string]int)
		// enforceMaxRounds=false, so no error even when count exceeds MaxRounds
		for i := range 5 {
			_, _, _, _, err := ProcessComponents(
				context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, roundCounts, false,
			)
			if err != nil {
				t.Fatalf("call %d should not error with enforceMaxRounds=false: %v", i, err)
			}
		}
		if roundCounts["repeating"] != 0 {
			t.Fatalf("roundCounts should not be incremented when enforceMaxRounds=false, got %d", roundCounts["repeating"])
		}
	})

	t.Run("empty component set returns not triggered", func(t *testing.T) {
		comps := ComponentSet{}
		_, _, _, triggered, err := ProcessComponents(
			context.Background(), comps, nil, nil, nil, nets.HTTPClient{}, nil, false,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if triggered {
			t.Fatal("expected triggered=false for empty component set")
		}
	})

	t.Run("returns remaining unmatched blocks", func(t *testing.T) {
		allBlocks := []blocks.Block{
			{Kind: "shell", Body: "echo hello"},
			{Kind: "unknown", Body: "test"},
		}

		comps := ComponentSet{
			{
				Kind: "shell",
				Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
					return ProcessResult{Parts: []generators.Part{generators.Text("shell output")}}
				},
			},
		}

		remaining, _, _, _, err := ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, nil, false,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(remaining) != 1 {
			t.Fatalf("expected 1 remaining block, got %d", len(remaining))
		}
		if remaining[0].Kind != "unknown" {
			t.Fatalf("expected remaining block kind 'unknown', got %s", remaining[0].Kind)
		}
	})
}
