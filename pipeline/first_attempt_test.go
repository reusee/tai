package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/records"
)

// withGoalRun resolves the run from a scope that carries the given goal
// loop number, so Module.Run enables the first-attempt abandonment
// policy the way a goal loop does. See TheoryOfFirstAttemptAbandonment.
func withGoalRun(t *testing.T, loop int, fn func(Run)) {
	t.Helper()
	run := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() *records.Recorder { return nil },
		func() GoalLoop { return GoalLoop(loop) },
	).Get[Run]()
	fn(run)
}

// TestRunFirstAttemptAbandonedOnMissingSummary verifies that a goal
// loop's first attempt without the summary block is abandoned instead of
// recovered: no handoff request is sent and no retry attempt runs. See
// TheoryOfFirstAttemptAbandonment.
func TestRunFirstAttemptAbandonedOnMissingSummary(t *testing.T) {
	withGoalRun(t, 1, func(run Run) {
		callCount := 0
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				callCount++
				return appendPhase("incomplete output without summary")
			},
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				t.Fatal("the first-attempt policy must not request a handoff")
				return nil, nil
			},
		})
		var abandoned *FirstAttemptAbandonedError
		if !errors.As(err, &abandoned) {
			t.Fatalf("expected FirstAttemptAbandonedError, got %v", err)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 attempt, got %d", callCount)
		}
		if !strings.Contains(abandoned.Reason, "summary block") {
			t.Fatalf("the reason must name the missing summary block, got %q", abandoned.Reason)
		}
	})
}

// TestRunFirstAttemptAbandonedOnError verifies that a goal loop's first
// attempt that fails after producing content is abandoned instead of
// retried with a handoff, and that the abandonment unwraps to the
// attempt's own error. See TheoryOfFirstAttemptAbandonment.
func TestRunFirstAttemptAbandonedOnError(t *testing.T) {
	withGoalRun(t, 1, func(run Run) {
		cause := errors.New("something went wrong")
		callCount := 0
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				callCount++
				return appendThenErrorPhase("partial model output", cause)
			},
			RetryOnError: true,
			MaxRetries:   3,
			Handoff: func(text string) (*Handoff, error) {
				t.Fatal("the first-attempt policy must not request a handoff")
				return nil, nil
			},
		})
		var abandoned *FirstAttemptAbandonedError
		if !errors.As(err, &abandoned) {
			t.Fatalf("expected FirstAttemptAbandonedError, got %v", err)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 attempt, got %d", callCount)
		}
		if !errors.Is(err, cause) {
			t.Fatalf("the abandonment must unwrap to the attempt error, got %v", err)
		}
	})
}

// TestRunFirstAttemptAbandonedOnParseError verifies that a goal loop's
// first attempt whose blocks are malformed is abandoned instead of
// corrected: the correction round would re-send the same large context to
// repair a format fault. See TheoryOfFirstAttemptAbandonment.
func TestRunFirstAttemptAbandonedOnParseError(t *testing.T) {
	withGoalRun(t, 1, func(run Run) {
		callCount := 0
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				callCount++
				return appendPhaseWithFlush("《龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n")
			},
		})
		var abandoned *FirstAttemptAbandonedError
		if !errors.As(err, &abandoned) {
			t.Fatalf("expected FirstAttemptAbandonedError, got %v", err)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 attempt with no correction round, got %d", callCount)
		}
		if !strings.Contains(abandoned.Reason, "correction round") {
			t.Fatalf("the reason must name the correction round, got %q", abandoned.Reason)
		}
	})
}

// TestRunFirstAttemptPolicySparesLaterAttempts verifies that the policy
// abandons only the run's first attempt: a loop that already produced
// conforming output keeps the ordinary retry path for its later attempts.
// See TheoryOfFirstAttemptAbandonment.
func TestRunFirstAttemptPolicySparesLaterAttempts(t *testing.T) {
	withGoalRun(t, 1, func(run Run) {
		callCount := 0
		comps := components.ComponentSet{
			{
				Kind: "go-src",
				Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
					return components.ProcessResult{
						Parts: []generators.Part{generators.Text("resolved source")},
					}
				},
			},
		}
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			HTTPClient:   nets.HTTPClient{},
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				callCount++
				switch callCount {
				case 1:
					return appendPhase("《建安 go-src\nfoo.Bar\n建安\n《贞观 summary\nDone.\n贞观\n")
				case 2:
					return appendPhase("incomplete output without summary")
				}
				return appendPhase("《贞观 summary\nDone.\n贞观\n")
			},
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
			},
		})
		if err != nil {
			t.Fatalf("the first-attempt policy must spare later attempts, got %v", err)
		}
		if callCount != 3 {
			t.Fatalf("expected 3 attempts (one spared retry), got %d", callCount)
		}
	})
}

// TestRunFirstAttemptPolicyDisabledWithoutGoalLoop verifies that a run
// outside a goal loop keeps the ordinary recovery path: the scope's zero
// GoalLoop leaves AbortOnFirstAttemptFailure unset. See
// TheoryOfFirstAttemptAbandonment.
func TestRunFirstAttemptPolicyDisabledWithoutGoalLoop(t *testing.T) {
	withGoalRun(t, 0, func(run Run) {
		callCount := 0
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendPhase("incomplete output without summary")
				}
				return appendPhase("《贞观 summary\nDone.\n贞观\n")
			},
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected the ordinary retry (2 attempts), got %d", callCount)
		}
	})
}

// TestRunFirstAttemptAbandonedOnCommitFailure verifies that a goal
// loop's first attempt which fails while committing its changes requests
// no handoff: the run still ends with the disk-change error whose
// feedback tells the next loop to reload the filesystem, but the error
// carries no handoff. See TheoryOfFirstAttemptAbandonment.
func TestRunFirstAttemptAbandonedOnCommitFailure(t *testing.T) {
	withGoalRun(t, 1, func(run Run) {
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("《贞观 summary\nDone.\n贞观\n")
			},
			OnAttemptSuccess: func(state generators.State, summaries []string) error {
				return &changes.DiskChangedError{Path: "a.go"}
			},
			Handoff: func(text string) (*Handoff, error) {
				t.Fatal("the first-attempt policy must not request a handoff")
				return nil, nil
			},
		})
		var diskChange *DiskChangeHandoffError
		if !errors.As(err, &diskChange) {
			t.Fatalf("expected DiskChangeHandoffError, got %v", err)
		}
		if diskChange.Handoff != nil {
			t.Fatal("the first-attempt policy must not produce a handoff")
		}
	})
}

// TestRunFirstAttemptAbandonedOnDiskChangeApplyError verifies that a goal
// loop's first attempt whose change block failed on a disk change keeps
// the cause's own terminal type with no handoff, so the runner still sends
// the next loop the reload notice and still exempts the cause from the
// consecutive-error bound. See TheoryOfFirstAttemptAbandonment and
// TheoryOfDiskChangeHandoff.
func TestRunFirstAttemptAbandonedOnDiskChangeApplyError(t *testing.T) {
	withGoalRun(t, 1, func(run Run) {
		phaseCalls := 0
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				phaseCalls++
				return appendThenErrorPhase(
					"partial model output",
					&changes.ApplyError{Err: &changes.DiskChangedError{Path: "a.go"}},
				)
			},
			RetryOnError: true,
			MaxRetries:   3,
			Handoff: func(text string) (*Handoff, error) {
				t.Fatal("the first-attempt policy must not request a handoff")
				return nil, nil
			},
		})
		if phaseCalls != 1 {
			t.Fatalf("expected 1 attempt, got %d", phaseCalls)
		}
		var diskChange *DiskChangeHandoffError
		if !errors.As(err, &diskChange) {
			t.Fatalf("expected DiskChangeHandoffError, got %v", err)
		}
		if diskChange.Handoff != nil {
			t.Fatal("the first-attempt policy must not produce a handoff")
		}
		var changed *changes.DiskChangedError
		if !errors.As(err, &changed) {
			t.Fatalf("the error must keep the disk change cause, got %v", err)
		}
	})
}

// TestRunFirstAttemptAbandonedOnContextExceeded verifies that a goal
// loop's first attempt that exceeded the model's context window keeps
// ContextExceededError with no handoff, so the runner still counts the
// cause toward the consecutive-error bound and still asks the next loop
// for a smaller, freshly assembled context. See
// TheoryOfFirstAttemptAbandonment and TheoryOfContextExceededHandoff.
func TestRunFirstAttemptAbandonedOnContextExceeded(t *testing.T) {
	withGoalRun(t, 1, func(run Run) {
		phaseCalls := 0
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				phaseCalls++
				return appendThenErrorPhase(
					"partial model output",
					errors.New("Error 400: This model's maximum context length is 128000 tokens. However, you requested 130000 tokens."),
				)
			},
			RetryOnError: true,
			MaxRetries:   3,
			Handoff: func(text string) (*Handoff, error) {
				t.Fatal("the first-attempt policy must not request a handoff")
				return nil, nil
			},
		})
		if phaseCalls != 1 {
			t.Fatalf("expected 1 attempt, got %d", phaseCalls)
		}
		var exceeded *ContextExceededError
		if !errors.As(err, &exceeded) {
			t.Fatalf("expected ContextExceededError, got %v", err)
		}
		if exceeded.Handoff != nil {
			t.Fatal("the first-attempt policy must not produce a handoff")
		}
	})
}
