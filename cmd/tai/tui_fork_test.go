package main

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
)

func TestForkTUIDisplayForwardsEventsToTUI(t *testing.T) {
	tui := newTUIForTest()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)

	scope.Call(func(run pipeline.Run) {
		usage := generators.Usage{}
		usage.Prompt.TokenCount = 100
		usage.Prompt.TokenCountCached = 20
		usage.Candidates.TokenCount = 50
		usage.Thoughts.TokenCount = 10

		var result pipeline.Result
		var usageEvents []pipeline.Event
		for ev, err := range run(context.Background(), pipeline.RunOptions{
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(_ generators.Generator) generators.Phase {
				return func(_ context.Context, state generators.State) (generators.Phase, generators.State, error) {
					state, err := state.AppendContent(&generators.Content{
						Role: generators.RoleModel,
						Parts: []generators.Part{
							generators.Text("ok\n"),
							usage,
						},
					})
					if err != nil {
						return nil, nil, err
					}
					return nil, state, nil
				}
			},
		}, &result) {
			if err != nil {
				t.Fatal(err)
			}
			if ev.Kind == pipeline.EventUsage {
				usageEvents = append(usageEvents, ev)
			}
		}
		if len(usageEvents) != 1 {
			t.Fatalf("expected 1 EventUsage in the event stream, got %d", len(usageEvents))
		}
		if got := usageEvents[0].Usage.Prompt.TokenCount; got != 100 {
			t.Fatalf("expected the attempt usage on EventUsage, got prompt tokens %d", got)
		}
		if got := usageEvents[0].Attempt; got != 1 {
			t.Fatalf("expected attempt 1 on EventUsage, got %d", got)
		}
	})

	tui.mu.Lock()
	defer tui.mu.Unlock()
	var texts []string
	for _, group := range tui.events {
		for _, line := range group {
			texts = append(texts, line.Text)
		}
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "[Usage] attempt 1") {
		t.Fatalf("expected the usage line in the Events tab, got %v", texts)
	}
	if !strings.Contains(joined, "prompt 100") || !strings.Contains(joined, "thoughts 10") {
		t.Fatalf("expected the usage counters in the Events tab, got %v", texts)
	}
}
