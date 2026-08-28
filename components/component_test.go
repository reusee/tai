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
	if got != "prompt-a\n\nprompt-b\n\nprompt-only\n\n" {
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

func TestSystemPromptRestate(t *testing.T) {
	// The restate repeats the full system prompt verbatim under a short
	// re-read instruction, so the reminder can never drift out of sync
	// with the instructions, and it ends with a blank line so following
	// content starts a fresh paragraph. See TheoryOfComponents and
	// generators.TheoryOfContentUnitSeparation.
	const prompt = "rule one\nrule two\n\n"
	part := SystemPromptRestate(prompt)
	if !strings.HasPrefix(string(part), systemPromptRestateHeader) {
		t.Fatalf("restate must open with the re-read instruction header, got %q", string(part))
	}
	if !strings.Contains(string(part), "rule one\nrule two\n") {
		t.Fatal("restate must repeat the system prompt verbatim")
	}
	if !strings.HasSuffix(string(part), "\n\n") {
		t.Fatal("restate must end with a blank line so following content starts a fresh paragraph")
	}
}

func TestComponentSetProcessable(t *testing.T) {
	comps := ComponentSet{
		{Kind: "a", Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult { return ProcessResult{} }},
		{Kind: "b"}, // no Process: not included in Processable()
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
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{},
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
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{},
		)
		if err != testErr {
			t.Fatalf("expected testErr, got %v", err)
		}
	})

	t.Run("empty component set returns not triggered", func(t *testing.T) {
		comps := ComponentSet{}
		_, _, _, triggered, err := ProcessComponents(
			context.Background(), comps, nil, nil, nil, nets.HTTPClient{},
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
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{},
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

func TestProcessComponentsStateModificationTriggers(t *testing.T) {
	// A component that modifies State (like ingest) must trigger
	// a new generation, just like a component that produces Parts. The modified
	// state is returned as newState, and triggered is true. combinedParts
	// is empty because the state was modified directly, not via Parts.
	// See TheoryOfComponents.
	initialState := generators.NewPrompts("", nil)

	comps := ComponentSet{
		{
			Kind: "state-modifier",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				newState, err := pctx.State.AppendContent(&generators.Content{
					Role:  generators.RoleUser,
					Parts: []generators.Part{generators.Text("fetched context")},
				})
				if err != nil {
					return ProcessResult{Err: err}
				}
				return ProcessResult{State: newState}
			},
		},
	}

	allBlocks := []blocks.Block{
		{Kind: "state-modifier", Body: "request"},
	}

	remaining, newState, combinedParts, triggered, err := ProcessComponents(
		context.Background(), comps, allBlocks, initialState, nil, nets.HTTPClient{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !triggered {
		t.Fatal("expected triggered=true when component modifies State")
	}
	if len(combinedParts) != 0 {
		t.Fatalf("expected 0 combined parts for state-only trigger, got %d", len(combinedParts))
	}
	if len(remaining) != 0 {
		t.Fatalf("expected 0 remaining blocks, got %d", len(remaining))
	}
	found := false
	for c := range newState.Contents() {
		for _, p := range c.Parts {
			if text, ok := p.(generators.Text); ok && string(text) == "fetched context" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected fetched context in new state")
	}
}
