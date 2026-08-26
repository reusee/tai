package pipeline

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

// TestRunEventStream verifies the loop's event contract: every notable
// occurrence of a run flows to the consumer as one Event stream, with
// the terminal error (nil on success) arriving as the error component
// of the yields. The run's first attempt misses the summary block
// (truncation retry with a handoff) and its second attempt completes
// with a summary and a usage part; the test asserts the event sequence,
// the round attribution (the retry happens within round 1, so no second
// round-start appears), and the fields carried by each event. The
// truncated event does not repeat the handoff summary — EventHandoff
// already carried it. See TheoryOfLoopEvents.
func TestRunEventStream(t *testing.T) {
	withRun(t, func(run Run) {
		usage := generators.Usage{}
		usage.Prompt.TokenCount = 42
		usage.Candidates.TokenCount = 7

		callCount := 0
		opts := RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendPhase("incomplete output without summary")
				}
				return appendPhaseWithUsage("<<龘靐 summary\nDone.\n龘靐\n", usage)
			},
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "truncated summary", Prompt: "retry prompt"}, nil
			},
		}

		var result Result
		var events []Event
		var terminalErr error
		for ev, err := range run(context.Background(), opts, &result) {
			if err != nil {
				terminalErr = err
			}
			events = append(events, ev)
		}
		if terminalErr != nil {
			t.Fatalf("unexpected terminal error: %v", terminalErr)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 attempts (one retry), got %d", callCount)
		}

		var kinds []EventKind
		for _, ev := range events {
			kinds = append(kinds, ev.Kind)
		}
		wantKinds := []EventKind{
			EventRoundStart,
			EventHandoff,
			EventRoundTruncated,
			EventRoundSuccess,
			EventUsage,
		}
		if !slices.Equal(kinds, wantKinds) {
			t.Fatalf("expected event kinds %v, got %v", wantKinds, kinds)
		}

		// The retry is a re-execution of the phase chain within the same
		// round: every event belongs to round 1 and the retry events
		// carry the attempt number and budget.
		for _, ev := range events {
			if ev.Round != 1 {
				t.Fatalf("expected round 1 on every event, got %d on %s", ev.Round, ev.Kind)
			}
		}
		handoffEv := events[1]
		if handoffEv.Summary != "truncated summary" ||
			handoffEv.Attempt != 1 || handoffEv.MaxAttempts != 3 ||
			handoffEv.Handoff == nil || handoffEv.Handoff.Prompt != "retry prompt" {
			t.Fatalf("unexpected handoff event: %+v", handoffEv)
		}
		truncatedEv := events[2]
		if truncatedEv.Attempt != 1 || truncatedEv.MaxAttempts != 3 ||
			!strings.Contains(truncatedEv.Detail, "missing completion") {
			t.Fatalf("unexpected truncated event: %+v", truncatedEv)
		}
		if truncatedEv.Summary != "" {
			t.Fatalf("truncated event must not repeat the handoff summary (EventHandoff carries it), got %q", truncatedEv.Summary)
		}
		successEv := events[3]
		if len(successEv.Summaries) != 1 ||
			!strings.Contains(successEv.Summaries[0], "Done.") ||
			!strings.Contains(successEv.Summary, "Done.") {
			t.Fatalf("unexpected success event: %+v", successEv)
		}
		usageEv := events[4]
		if usageEv.Usage.Prompt.TokenCount != 42 || usageEv.Usage.Candidates.TokenCount != 7 {
			t.Fatalf("unexpected usage event: %+v", usageEv)
		}
	})
}

// eventSummaryGenerator is a minimal generators.Generator whose Generate
// returns a fixed summary block, so NewSummarizer can be exercised
// without a real model. See TestRunThoughtSummaryEvent.
type eventSummaryGenerator struct{}

func (eventSummaryGenerator) Spec() generators.Spec {
	return generators.Spec{}
}

func (eventSummaryGenerator) CountTokens(text string) (int, error) {
	return 0, nil
}

func (eventSummaryGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	return state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Text("<<齉爩 summary\n- condensed\n齉爩\n"),
		},
	})
}

// TestRunEmitsFinishEvent verifies that each generation attempt's finish
// reason flows to the event stream as an EventFinish (Detail carrying
// the reason), emitted before the round completes. See
// TheoryOfLoopEvents.
func TestRunEmitsFinishEvent(t *testing.T) {
	withRun(t, func(run Run) {
		opts := RunOptions{
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
					newState, err := state.AppendContent(&generators.Content{
						Role: generators.RoleLog,
						Parts: []generators.Part{
							generators.FinishReason("stop"),
							generators.Text("<<齉爩 summary\n- done\n齉爩\n"),
						},
					})
					if err != nil {
						return nil, state, err
					}
					return nil, newState, nil
				}
			},
		}

		var result Result
		var events []Event
		var terminalErr error
		for ev, err := range run(context.Background(), opts, &result) {
			if err != nil {
				terminalErr = err
			}
			events = append(events, ev)
		}
		if terminalErr != nil {
			t.Fatalf("unexpected terminal error: %v", terminalErr)
		}

		var kinds []EventKind
		for _, ev := range events {
			kinds = append(kinds, ev.Kind)
		}
		wantKinds := []EventKind{
			EventRoundStart,
			EventFinish,
			EventRoundSuccess,
		}
		if !slices.Equal(kinds, wantKinds) {
			t.Fatalf("expected event kinds %v, got %v", wantKinds, kinds)
		}
		finishEv := events[1]
		if finishEv.Detail != "stop" || finishEv.Round != 1 {
			t.Fatalf("unexpected finish event: %+v", finishEv)
		}
	})
}

// TestRunThoughtSummaryEvent verifies that a thought summary produced by
// the ThoughtsSummarize state layer during generation flows to the event
// stream as an EventThoughtSummary: Module.Run installs the layer's
// emitter onto the guarded yield, so the summary joins the run's single
// event channel. See TheoryOfLoopEvents and TheoryOfThoughtsSummarize.
func TestRunThoughtSummaryEvent(t *testing.T) {
	withRun(t, func(run Run) {
		summarizer := NewSummarizer(eventSummaryGenerator{})
		initial := NewThoughtsSummarize(
			context.Background(),
			generators.NewPrompts("", nil),
			summarizer,
			&strings.Builder{},
		)
		opts := RunOptions{
			InitialState: initial,
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
					// A mixed content (thought plus answer text) flushes
					// the accumulated thoughts immediately, producing one
					// summary during phase execution.
					newState, err := state.AppendContent(&generators.Content{
						Role: generators.RoleModel,
						Parts: []generators.Part{
							generators.Thought("long reasoning\n"),
							generators.Text("answer\n"),
						},
					})
					if err != nil {
						return nil, state, err
					}
					return nil, newState, nil
				}
			},
		}

		var result Result
		var kinds []EventKind
		var summary string
		var terminalErr error
		for ev, err := range run(context.Background(), opts, &result) {
			if err != nil {
				terminalErr = err
			}
			kinds = append(kinds, ev.Kind)
			if ev.Kind == EventThoughtSummary {
				summary = ev.Summary
			}
		}
		if terminalErr != nil {
			t.Fatalf("unexpected terminal error: %v", terminalErr)
		}

		wantKinds := []EventKind{
			EventRoundStart,
			EventThoughtSummary,
			EventRoundSuccess,
		}
		if !slices.Equal(kinds, wantKinds) {
			t.Fatalf("expected event kinds %v, got %v", wantKinds, kinds)
		}
		if summary != "- condensed" {
			t.Fatalf("unexpected thought summary: %q", summary)
		}
	})
}
