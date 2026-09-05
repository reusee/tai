package components

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/tree"
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

func TestSystemPromptRestateForUserPrompt(t *testing.T) {
	// countBytes is a deterministic stand-in tokenizer: one token per
	// byte, so the threshold is crossed by fixture text larger than
	// SystemPromptRestateThreshold bytes.
	countBytes := func(text string) (int, error) {
		return len(text), nil
	}

	t.Run("within threshold omits restate", func(t *testing.T) {
		parts := []generators.Part{
			generators.Text("short context\n"),
			generators.Thought("reasoning is not counted"),
		}
		restate, tokens, err := SystemPromptRestateForUserPrompt(parts, "rules", countBytes)
		if err != nil {
			t.Fatal(err)
		}
		if len(restate) != 0 {
			t.Fatal("restate must be omitted for a user prompt within the threshold")
		}
		if want := len("short context\n"); tokens != want {
			t.Fatalf("tokens = %d, want %d (Text parts only)", tokens, want)
		}
	})

	t.Run("above threshold returns restate", func(t *testing.T) {
		long := strings.Repeat("x", SystemPromptRestateThreshold+1)
		parts := []generators.Part{generators.Text(long)}
		restate, tokens, err := SystemPromptRestateForUserPrompt(parts, "rules", countBytes)
		if err != nil {
			t.Fatal(err)
		}
		if len(restate) != 1 {
			t.Fatalf("expected the restate part above the threshold, got %d parts", len(restate))
		}
		if restate[0] != SystemPromptRestate("rules") {
			t.Fatal("restate part must be the verbatim system prompt restate")
		}
		if tokens != len(long) {
			t.Fatalf("tokens = %d, want %d", tokens, len(long))
		}
	})

	t.Run("count error propagates", func(t *testing.T) {
		_, _, err := SystemPromptRestateForUserPrompt(
			[]generators.Part{generators.Text("x")},
			"rules",
			func(string) (int, error) { return 0, generators.ErrRetryable },
		)
		if err == nil {
			t.Fatal("count error must propagate")
		}
	})
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

		remaining, _, combinedParts, outputs, _, triggered, err := ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, nil,
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
		// Each component's output carries its consumed blocks with their
		// original indexes, so the loop can attach block-result nodes to
		// the right session-tree nodes.
		if len(outputs) != 2 {
			t.Fatalf("expected 2 outputs, got %d", len(outputs))
		}
		if outputs[0].Kind != "shell" || len(outputs[0].Blocks) != 1 || outputs[0].BlockIndexes[0] != 0 {
			t.Fatalf("unexpected shell output: %+v", outputs[0])
		}
		if outputs[1].Kind != "continue" || len(outputs[1].Blocks) != 1 || outputs[1].BlockIndexes[0] != 1 {
			t.Fatalf("unexpected continue output: %+v", outputs[1])
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

		_, _, _, _, _, _, err := ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, nil,
		)
		if err != testErr {
			t.Fatalf("expected testErr, got %v", err)
		}
	})

	t.Run("empty component set returns not triggered", func(t *testing.T) {
		comps := ComponentSet{}
		_, _, _, _, _, triggered, err := ProcessComponents(
			context.Background(), comps, nil, nil, nil, nets.HTTPClient{}, nil,
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

		remaining, _, _, _, _, _, err := ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, nil,
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

	t.Run("threads the session tree through components", func(t *testing.T) {
		base := tree.New()
		comps := ComponentSet{
			{
				Kind: "writer",
				Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
					next, err := pctx.SessionTree.Write("root", "n1", tree.Type("writer"), tree.AuthorModel, "x")
					if err != nil {
						return ProcessResult{Err: err}
					}
					return ProcessResult{Parts: []generators.Part{generators.Text("ok")}, Tree: next}
				},
			},
		}
		allBlocks := []blocks.Block{{Kind: "writer", Body: "b"}}
		_, _, parts, _, treeOut, triggered, err := ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, base,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !triggered || len(parts) != 1 {
			t.Fatalf("unexpected outcome: triggered=%v parts=%d", triggered, len(parts))
		}
		if treeOut == base {
			t.Fatal("the tree returned through ProcessResult.Tree must replace the input")
		}
		if _, ok := treeOut.Node("n1"); !ok {
			t.Fatal("the node written by the component is missing from the threaded tree")
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

	remaining, newState, combinedParts, _, _, triggered, err := ProcessComponents(
		context.Background(), comps, allBlocks, initialState, nil, nets.HTTPClient{}, nil,
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

func TestComponentSetComputes(t *testing.T) {
	// Computes maps the kinds that declare a side-effect-free per-block
	// computation; kinds without Compute, and prompt-only kinds, are
	// never prefetched. See TheoryOfReadOnlyPrefetch.
	computes := ComponentSet{
		{
			Kind: "go-src",
			Compute: func(ctx context.Context, block blocks.Block, root *os.Root, httpClient nets.HTTPClient) ([]generators.Part, error) {
				return nil, nil
			},
		},
		{Kind: "shell"}, // no Compute: never prefetched
		{Kind: ""},      // prompt-only: never prefetched
	}.Computes()
	if len(computes) != 1 {
		t.Fatalf("expected 1 compute entry, got %d", len(computes))
	}
	if _, ok := computes["go-src"]; !ok {
		t.Fatal("expected the go-src kind in the computes map")
	}
}

func TestStartPrefetchDeliversOutcome(t *testing.T) {
	// StartPrefetch runs the computation in a background goroutine and
	// delivers its outcome through the buffered future. See
	// TheoryOfReadOnlyPrefetch.
	future := StartPrefetch(func() Prefetched {
		return Prefetched{Parts: []generators.Part{generators.Text("computed")}}
	})
	outcome := future.Wait()
	if outcome.Err != nil {
		t.Fatalf("unexpected error: %v", outcome.Err)
	}
	if len(outcome.Parts) != 1 || string(outcome.Parts[0].(generators.Text)) != "computed" {
		t.Fatalf("unexpected parts: %v", outcome.Parts)
	}
}

func TestStartPrefetchRecoversPanic(t *testing.T) {
	// A panicking computation is recovered and delivered as an error, so
	// a prefetch panic never wedges the consumer nor crashes the
	// generation loop. See TheoryOfReadOnlyPrefetch.
	future := StartPrefetch(func() Prefetched {
		panic("boom")
	})
	outcome := future.Wait()
	if outcome.Err == nil {
		t.Fatal("expected the panicking computation to be delivered as an error")
	}
}

func TestProcessComponentsConsumesPrefetched(t *testing.T) {
	// Futures align with block positions: a prefetched block delivers
	// its own outcome, a non-prefetched block falls back to a
	// synchronous Compute call, and nothing is computed twice. See
	// TheoryOfReadOnlyPrefetch.
	computed := 0
	compute := func(ctx context.Context, block blocks.Block, root *os.Root, httpClient nets.HTTPClient) ([]generators.Part, error) {
		computed++
		return []generators.Part{generators.Text("sync " + block.Body)}, nil
	}
	comps := ComponentSet{
		{
			Kind:    "go-src",
			Compute: compute,
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				var parts []generators.Part
				for i := range pctx.Blocks {
					if i < len(pctx.Prefetched) && pctx.Prefetched[i] != nil {
						outcome := pctx.Prefetched[i].Wait()
						parts = append(parts, outcome.Parts...)
						continue
					}
					p, err := compute(ctx, pctx.Blocks[i], pctx.Root, pctx.HttpClient)
					if err != nil {
						return ProcessResult{Err: err}
					}
					parts = append(parts, p...)
				}
				return ProcessResult{Parts: parts}
			},
		},
	}
	allBlocks := []blocks.Block{
		{Kind: "go-src", Body: "one"},
		{Kind: "go-src", Body: "two"},
	}
	prefetched := []PrefetchFuture{
		StartPrefetch(func() Prefetched {
			return Prefetched{Parts: []generators.Part{generators.Text("future one")}}
		}),
		// Block two is not prefetched: the component computes it
		// synchronously through Compute.
		nil,
	}

	_, _, combinedParts, _, _, triggered, err := ProcessComponents(
		context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, nil,
		prefetched...,
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
	if string(combinedParts[0].(generators.Text)) != "future one" ||
		string(combinedParts[1].(generators.Text)) != "sync two" {
		t.Fatalf("futures must align with block positions, got %v", combinedParts)
	}
	if computed != 1 {
		t.Fatalf("expected 1 synchronous compute (the non-prefetched block), got %d", computed)
	}
}

func TestProcessComponentsPrefetchAlignmentAcrossKinds(t *testing.T) {
	// A go-src block followed by an ingest block: the go-src component
	// (registered before ingest) removes its block, shifting the ingest
	// block's index left. The ingest component must receive the ingest
	// block's own future; a lookup by the shifted index hands it the
	// already-consumed go-src future, and the second Wait deadlocks —
	// the attempt finishes but the run never continues. See
	// TheoryOfReadOnlyPrefetch.
	comps := ComponentSet{
		{
			Kind: "go-src",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				return ProcessResult{Parts: waitAllPrefetched(pctx)}
			},
		},
		{
			Kind: "ingest",
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				return ProcessResult{Parts: waitAllPrefetched(pctx)}
			},
		},
	}
	allBlocks := []blocks.Block{
		{Kind: "go-src", Body: "symbol"},
		{Kind: "ingest", Body: "file"},
	}
	prefetched := []PrefetchFuture{
		StartPrefetch(func() Prefetched {
			return Prefetched{Parts: []generators.Part{generators.Text("gosrc result")}}
		}),
		StartPrefetch(func() Prefetched {
			return Prefetched{Parts: []generators.Part{generators.Text("ingest result")}}
		}),
	}

	type outcome struct {
		parts []generators.Part
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		_, _, parts, _, _, _, err := ProcessComponents(
			context.Background(), comps, allBlocks, nil, nil, nets.HTTPClient{}, nil,
			prefetched...,
		)
		done <- outcome{parts: parts, err: err}
	}()
	select {
	case o := <-done:
		if o.err != nil {
			t.Fatalf("unexpected error: %v", o.err)
		}
		if len(o.parts) != 2 {
			t.Fatalf("expected 2 parts, got %d", len(o.parts))
		}
		if string(o.parts[0].(generators.Text)) != "gosrc result" ||
			string(o.parts[1].(generators.Text)) != "ingest result" {
			t.Fatalf("each kind must consume its own future, got %v", o.parts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ProcessComponents deadlocked: a component consumed another kind's already-consumed future")
	}
}

func waitAllPrefetched(pctx *ProcessContext) []generators.Part {
	var parts []generators.Part
	for i := range pctx.Blocks {
		if i < len(pctx.Prefetched) && pctx.Prefetched[i] != nil {
			outcome := pctx.Prefetched[i].Wait()
			parts = append(parts, outcome.Parts...)
			continue
		}
		parts = append(parts, generators.Text("sync "+pctx.Blocks[i].Body))
	}
	return parts
}
