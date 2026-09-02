package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gotools"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline/codetypes"
)

func TestSystemPromptGoSrcBlock(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		s := string(prompt)
		if !strings.Contains(s, "Go-Src Block Kind") {
			t.Fatal("system prompt must include go-src block section")
		}
		if !strings.Contains(s, "go-src block is NOT a completion signal") {
			t.Fatal("system prompt must state that go-src block is not a completion signal and summary is still required")
		}
	})
}

func TestGoSrcComponentResolvesSymbols(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() gotools.ResolveGoSymbols {
			return func(symbols []string) ([]generators.Part, error) {
				return []generators.Part{generators.Text("resolved: " + strings.Join(symbols, ","))}, nil
			}
		},
	).Call(func(
		comps CodesComponents,
	) {
		for _, comp := range comps.Processable() {
			if comp.Kind != "go-src" {
				continue
			}
			result := comp.Process(context.Background(), &components.ProcessContext{
				Blocks: []blocks.Block{
					{Kind: "go-src", Body: "Foo\nBar.Read"},
					{Kind: "go-src", Body: "*Baz.Write"},
				},
			})
			if result.Err != nil {
				t.Fatalf("unexpected error: %v", result.Err)
			}
			// Per-block computation: each block carries its own header
			// and its own resolution, in block order. See
			// components.TheoryOfReadOnlyPrefetch.
			if len(result.Parts) != 4 {
				t.Fatalf("expected 4 parts (2 headers + 2 resolutions), got %d", len(result.Parts))
			}
			if !strings.Contains(string(result.Parts[0].(generators.Text)), "Requested source") {
				t.Fatalf("unexpected header part: %q", result.Parts[0])
			}
			if text := string(result.Parts[1].(generators.Text)); text != "resolved: Foo,Bar.Read" {
				t.Fatalf("unexpected resolved text: %q", text)
			}
			if !strings.Contains(string(result.Parts[2].(generators.Text)), "Requested source") {
				t.Fatalf("unexpected header part: %q", result.Parts[2])
			}
			if text := string(result.Parts[3].(generators.Text)); text != "resolved: *Baz.Write" {
				t.Fatalf("unexpected resolved text: %q", text)
			}
			return
		}
		t.Fatal("go-src component not found")
	})
}

func TestGoSrcComponentEmptyBodyFeedback(t *testing.T) {
	resolverCalled := false
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() gotools.ResolveGoSymbols {
			return func(symbols []string) ([]generators.Part, error) {
				resolverCalled = true
				return nil, nil
			}
		},
	).Call(func(
		comps CodesComponents,
	) {
		for _, comp := range comps.Processable() {
			if comp.Kind != "go-src" {
				continue
			}
			result := comp.Process(context.Background(), &components.ProcessContext{
				Blocks: []blocks.Block{{Kind: "go-src", Body: "  \n"}},
			})
			if result.Err != nil {
				t.Fatalf("unexpected error: %v", result.Err)
			}
			if len(result.Parts) != 1 {
				t.Fatalf("expected 1 feedback part, got %d", len(result.Parts))
			}
			if !strings.Contains(string(result.Parts[0].(generators.Text)), "one Go symbol per line") {
				t.Fatalf("unexpected feedback: %q", result.Parts[0])
			}
			// The feedback ends with a blank line so consecutive parts in
			// the same round stay paragraph-separated. See
			// generators.TheoryOfContentUnitSeparation.
			if !strings.HasSuffix(string(result.Parts[0].(generators.Text)), "\n\n") {
				t.Fatalf("feedback part must end with a blank line, got %q", result.Parts[0])
			}
			return
		}
		t.Fatal("go-src component not found")
	})
	if resolverCalled {
		t.Fatal("the resolver must not be called for an empty symbol list")
	}
}
