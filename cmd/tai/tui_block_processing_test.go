package main

import (
	"context"
	"iter"
	"testing"

	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/tree"
)

// recordingBlockObserver records the processing lifecycle for assertions.
type recordingBlockObserver struct {
	events []string
}

func (o *recordingBlockObserver) BlockProcessingStart(kind string) {
	o.events = append(o.events, "start:"+kind)
}

func (o *recordingBlockObserver) BlockProcessingEnd() {
	o.events = append(o.events, "end")
}

func TestObservedComponentsReportsProcessing(t *testing.T) {
	observer := new(recordingBlockObserver)
	ran := 0
	comps := components.ComponentSet{
		{
			Kind: "go-test",
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				ran++
				return components.ProcessResult{Parts: []generators.Part{generators.Text("ok")}}
			},
		},
		{
			Kind:          "summary",
			PromptSection: "summary prompt",
		},
	}
	observed := observedComponents(comps, observer)
	if len(observed) != len(comps) {
		t.Fatalf("expected %d components, got %d", len(comps), len(observed))
	}
	// A prompt-only component is copied unchanged and never reported.
	if observed[1].Process != nil || observed[1].Kind != "summary" || observed[1].PromptSection != "summary prompt" {
		t.Fatalf("the prompt-only component changed: %+v", observed[1])
	}
	result := observed[0].Process(context.Background(), &components.ProcessContext{})
	if result.Err != nil || ran != 1 || len(result.Parts) != 1 {
		t.Fatalf("unexpected process outcome: %+v ran=%d", result, ran)
	}
	want := []string{"start:go-test", "end"}
	if len(observer.events) != len(want) || observer.events[0] != want[0] || observer.events[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, observer.events)
	}
	// The original set is not mutated: its Process runs without reporting.
	if result := comps[0].Process(context.Background(), &components.ProcessContext{}); result.Err != nil {
		t.Fatal(result.Err)
	}
	if len(observer.events) != len(want) {
		t.Fatalf("the original component must not report, got %v", observer.events)
	}
	// A nil observer leaves the set untouched.
	if got := observedComponents(comps, nil); len(got) != len(comps) {
		t.Fatalf("expected %d components, got %d", len(comps), len(got))
	}
}

func TestTUIBlockProcessingLifecycle(t *testing.T) {
	tui := newTUIForTest()
	tui.BlockProcessingStart("go-test")
	if tui.blockProcessing != "go-test" {
		t.Fatalf("expected the processing kind, got %q", tui.blockProcessing)
	}
	if label := outputTabLabel(tui.finished, tui.generating, tui.handoff, tui.blockProcessing); label != "Output (processing go-test...)" {
		t.Fatalf("expected the processing label, got %q", label)
	}
	// Processing outranks a stale generating hint: a request that ends
	// without a finish node leaves generating set while a component still
	// works.
	tui.generating = true
	if label := outputTabLabel(tui.finished, tui.generating, tui.handoff, tui.blockProcessing); label != "Output (processing go-test...)" {
		t.Fatalf("expected the processing label to outrank generating, got %q", label)
	}
	tui.generating = false
	tui.BlockProcessingEnd()
	if tui.blockProcessing != "" {
		t.Fatalf("expected the processing state cleared, got %q", tui.blockProcessing)
	}
	// The session end clears a processing state left by a run that
	// stopped mid-processing.
	tui.BlockProcessingStart("ingest")
	tui.genEnd(nil)
	if tui.blockProcessing != "" {
		t.Fatalf("expected genEnd to clear the processing state, got %q", tui.blockProcessing)
	}
}

func TestWithTUIBlockProcessingWrapsComponents(t *testing.T) {
	tui := newTUIForTest()
	var gotOpts pipeline.RunOptions
	sampled := ""
	run := func(ctx context.Context, opts pipeline.RunOptions, result *pipeline.Result) iter.Seq2[*tree.Tree, error] {
		gotOpts = opts
		return func(yield func(*tree.Tree, error) bool) {}
	}
	for range withTUIBlockProcessing(run, tui)(context.Background(), pipeline.RunOptions{
		Components: components.ComponentSet{{
			Kind: "go-test",
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				// The wrapper reports the kind before the work runs, so
				// the title names the component the user waits on.
				sampled = tui.blockProcessing
				return components.ProcessResult{}
			},
		}},
	}, nil) {
	}
	if len(gotOpts.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(gotOpts.Components))
	}
	gotOpts.Components[0].Process(context.Background(), &components.ProcessContext{})
	if sampled != "go-test" {
		t.Fatalf("expected the processing kind during the work, got %q", sampled)
	}
	if tui.blockProcessing != "" {
		t.Fatalf("expected the processing state cleared after the work, got %q", tui.blockProcessing)
	}
}
