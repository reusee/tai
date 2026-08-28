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
// the attempt attribution (the retry happens within the same
// generation, so the second attempt-start carries attempt 2), and the
// fields carried by each event. The truncated event is emitted before
// the handoff request and does not repeat the handoff summary —
// EventHandoff already carries it. See TheoryOfLoopEvents.
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
			EventAttemptStart,
			EventTruncated,
			EventHandoffStart,
			EventHandoff,
			EventAttemptStart,
			EventUsage,
			EventAttemptCompleted,
		}
		if !slices.Equal(kinds, wantKinds) {
			t.Fatalf("expected event kinds %v, got %v", wantKinds, kinds)
		}

		// The retry is a re-execution of the phase chain within the
		// same generation: the first attempt-start carries attempt 1
		// and the retry budget, the second carries attempt 2.
		startEv := events[0]
		if startEv.Attempt != 1 || startEv.MaxAttempts != 3 {
			t.Fatalf("unexpected first attempt-start event: %+v", startEv)
		}
		truncatedEv := events[1]
		if truncatedEv.Attempt != 1 || truncatedEv.MaxAttempts != 3 ||
			!strings.Contains(truncatedEv.Detail, "missing completion") {
			t.Fatalf("unexpected truncated event: %+v", truncatedEv)
		}
		if truncatedEv.Summary != "" {
			t.Fatalf("truncated event must not repeat the handoff summary (EventHandoff carries it), got %q", truncatedEv.Summary)
		}
		// Handoff events carry the attempt attribution but no budget
		// figures: handoff generation itself retries without an
		// attempt limit, so a budget display would misrepresent it.
		// See TheoryOfHandoff and TheoryOfLoopEvents.
		handoffStartEv := events[2]
		if handoffStartEv.Attempt != 1 ||
			handoffStartEv.AttemptInGeneration != 0 || handoffStartEv.MaxAttempts != 0 {
			t.Fatalf("unexpected handoff-start event: %+v", handoffStartEv)
		}
		handoffEv := events[3]
		if handoffEv.Summary != "truncated summary" ||
			handoffEv.Attempt != 1 ||
			handoffEv.AttemptInGeneration != 0 || handoffEv.MaxAttempts != 0 ||
			handoffEv.Handoff == nil || handoffEv.Handoff.Prompt != "retry prompt" {
			t.Fatalf("unexpected handoff event: %+v", handoffEv)
		}
		restartEv := events[4]
		if restartEv.Attempt != 2 || restartEv.MaxAttempts != 3 {
			t.Fatalf("unexpected second attempt-start event: %+v", restartEv)
		}
		usageEv := events[5]
		if usageEv.Attempt != 2 ||
			usageEv.Usage.Prompt.TokenCount != 42 || usageEv.Usage.Candidates.TokenCount != 7 {
			t.Fatalf("unexpected usage event: %+v", usageEv)
		}
		completedEv := events[6]
		if len(completedEv.Summaries) != 1 ||
			!strings.Contains(completedEv.Summaries[0], "Done.") ||
			!strings.Contains(completedEv.Summary, "Done.") {
			t.Fatalf("unexpected completed event: %+v", completedEv)
		}

		// A non-goal run carries no loop attribution: every event keeps
		// Loop 0. See TheoryOfLoopEvents.
		for _, ev := range events {
			if ev.Loop != 0 {
				t.Fatalf("expected Loop 0 on a non-goal run, got %+v", ev)
			}
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
// the reason), emitted immediately after the attempt's finish reason is
// known, before the attempt completes. See TheoryOfLoopEvents.
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
			EventAttemptStart,
			EventFinish,
			EventAttemptCompleted,
		}
		if !slices.Equal(kinds, wantKinds) {
			t.Fatalf("expected event kinds %v, got %v", wantKinds, kinds)
		}
		finishEv := events[1]
		if finishEv.Detail != "stop" || finishEv.Attempt != 1 {
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
			EventAttemptStart,
			EventThoughtSummary,
			EventAttemptCompleted,
		}
		if !slices.Equal(kinds, wantKinds) {
			t.Fatalf("expected event kinds %v, got %v", wantKinds, kinds)
		}
		if summary != "- condensed" {
			t.Fatalf("unexpected thought summary: %q", summary)
		}
	})
}

// TestRunEventsCarryLoop verifies that RunOptions.Loop is stamped onto
// every event the loop emits: a goal run's per-attempt events carry the
// goal loop number, so a consumer can attribute each attempt and usage
// to its loop. See TheoryOfLoopEvents.
func TestRunEventsCarryLoop(t *testing.T) {
	withRun(t, func(run Run) {
		opts := RunOptions{
			Loop:         7,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<齉爩 summary\n- done\n齉爩\n")
			},
		}
		var result Result
		var events []Event
		for ev, err := range run(context.Background(), opts, &result) {
			if err != nil {
				t.Fatalf("unexpected terminal error: %v", err)
			}
			events = append(events, ev)
		}
		if len(events) == 0 {
			t.Fatal("expected events")
		}
		for _, ev := range events {
			if ev.Loop != 7 {
				t.Fatalf("expected Loop 7 on every event, got %+v", ev)
			}
		}
	})
}

// TestRunEventsFormTree verifies the event tree contract: a goal run
// opens with the loop-start event, every attempt nests under it, and
// the attempt's lifecycle events nest under the attempt-start; the
// sequence numbers increase monotonically. A non-goal run emits no
// loop-start, so its attempt-start events are roots. See
// TheoryOfLoopEvents.
func TestRunEventsFormTree(t *testing.T) {
	withRun(t, func(run Run) {
		var result Result
		var events []Event
		opts := RunOptions{
			Loop:         2,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			},
		}
		for ev, err := range run(context.Background(), opts, &result) {
			if err != nil {
				t.Fatalf("unexpected terminal error: %v", err)
			}
			events = append(events, ev)
		}
		if len(events) < 2 || events[0].Kind != EventLoopStart {
			t.Fatalf("expected the loop-start event first, got %v", events)
		}
		root := events[0]
		if root.Seq != 1 || root.Parent != 0 || root.Loop != 2 {
			t.Fatalf("unexpected loop-start event: %+v", root)
		}
		attemptSeq := 0
		lastSeq := 0
		for _, ev := range events[1:] {
			if ev.Seq <= lastSeq {
				t.Fatalf("expected increasing sequence numbers, got %d after %d", ev.Seq, lastSeq)
			}
			lastSeq = ev.Seq
			if ev.Kind == EventAttemptStart {
				if ev.Parent != root.Seq {
					t.Fatalf("attempt-start must nest under the loop branch, got %+v", ev)
				}
				attemptSeq = ev.Seq
				continue
			}
			want := root.Seq
			if attemptSeq != 0 {
				want = attemptSeq
			}
			if ev.Parent != want {
				t.Fatalf("event %v must nest under %d, got %+v", ev.Kind, want, ev)
			}
		}

		// A non-goal run emits no loop-start: the attempt-start is the
		// root of its branch.
		var plain []Event
		for ev, err := range run(context.Background(), RunOptions{
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			},
		}, &result) {
			if err != nil {
				t.Fatalf("unexpected terminal error: %v", err)
			}
			plain = append(plain, ev)
		}
		if len(plain) == 0 || plain[0].Kind != EventAttemptStart || plain[0].Parent != 0 {
			t.Fatalf("expected the attempt-start as the root of a non-goal run, got %v", plain)
		}
	})
}
