package main

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/taiui"
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

func TestForkTUIDisplayDecoratesHandoffState(t *testing.T) {
	// The handoff decorator from the display scope must observe content
	// parts, so handoff output is highlighted per part and per thinking
	// state in the Output tab, the same as regular generation output.
	// See TheoryOfTUIHandoff.
	tui := newTUIForTest()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)

	scope.Call(func(decorate pipeline.HandoffStateDecorator) {
		state := decorate(generators.NewPrompts("", nil))
		_, err := state.AppendContent(&generators.Content{
			Role: generators.RoleModel,
			Parts: []generators.Part{
				generators.Thought("handoff thinking"),
				generators.Text("handoff text\n"),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	tui.mu.Lock()
	defer tui.mu.Unlock()
	lines := tui.output.Lines()
	if len(lines) == 0 {
		t.Fatal("expected lines in the output buffer")
	}
	if lines[0].Text != "handoff thinking" || lines[0].Color != outputColorThoughtLine {
		t.Fatalf("expected the thought line in the thought color, got %+v", lines[0])
	}
	found := false
	for _, line := range lines {
		if line.Text == "handoff text" {
			found = true
			if line.Color != taiui.NoColor {
				t.Fatalf("expected the text line in the default color, got %+v", lines)
			}
		}
	}
	if !found {
		t.Fatalf("expected the handoff text line in the output buffer, got %v", lines)
	}
}

// TestForkTUIDisplayForwardsGoalEventsToTUI verifies that the goal
// event observer forked by forkTUIDisplay forwards the goal runner's
// verdicts to the TUI, so the Events tab renders them without a writer
// fork. See TheoryOfTUIDisplayFork.
func TestForkTUIDisplayForwardsGoalEventsToTUI(t *testing.T) {
	tui := newTUIForTest()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)

	scope.Call(func(observe pipeline.GoalEventObserver) {
		if observe == nil {
			t.Fatal("expected the TUI fork to provide a goal event observer")
		}
		observe(pipeline.Event{Kind: pipeline.EventGoal, Detail: "[Goal Achieved after 2 loop(s)]"})
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
	if !strings.Contains(joined, "[Goal Achieved after 2 loop(s)]") {
		t.Fatalf("expected the goal verdict in the Events tab, got %v", texts)
	}
}
