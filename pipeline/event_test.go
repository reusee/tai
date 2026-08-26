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
// round-start appears), and the fields carried by each event.
// See TheoryOfLoopEvents.
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
		if truncatedEv.Summary != "truncated summary" ||
			truncatedEv.Attempt != 1 || truncatedEv.MaxAttempts != 3 ||
			!strings.Contains(truncatedEv.Detail, "missing completion") {
			t.Fatalf("unexpected truncated event: %+v", truncatedEv)
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
