package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
)

// TestForkTUIDisplayCancelsRunOnQuit pins the exit path: the display's
// cancellation decorator records the run's cancel function, so the quit
// path stops the generation; the cancelled run ends with the cancellation
// as its error after recording the attempt's already-received output, and
// the recovery steps do not run. See TheoryOfTUIExit and
// pipeline.TheoryOfRunCancellation.
func TestForkTUIDisplayCancelsRunOnQuit(t *testing.T) {
	// The handle latches the quit, so reset it for the tests that follow.
	// See TheoryOfTUIExit.
	defer func() { tuiRunCancel = runCancellation{} }()
	tui := newTUIForTest()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)
	run := scope.Get[pipeline.Run]()
	entered := make(chan struct{}, 1)
	var result pipeline.Result
	var runErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, e := range run(context.Background(), pipeline.RunOptions{
			InitialState: generators.NewPrompts("", nil),
			// The retry path is enabled on purpose: a cancelled session
			// must break it instead of retrying the interrupted attempt.
			RetryOnError: true,
			PhaseBuilder: func(_ generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
					select {
					case entered <- struct{}{}:
					default:
					}
					newState, err := state.AppendContent(&generators.Content{
						Role:  generators.RoleModel,
						Parts: []generators.Part{generators.Text("partial output")},
					})
					if err != nil {
						return nil, state, err
					}
					<-ctx.Done()
					return nil, newState, errors.New("phase aborted")
				}
			},
		}, &result) {
			if e != nil {
				runErr = e
			}
		}
	}()
	<-entered
	tuiRunCancel.cancelRun()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the cancelled run")
	}
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("expected the cancellation error, got %v", runErr)
	}
	if result.SessionTree == nil {
		t.Fatal("expected the session tree")
	}
	outline := result.SessionTree.RenderSubtree("root", 120)
	if !strings.Contains(outline, "partial output") {
		t.Fatalf("expected the partial output in the session tree, got %q", outline)
	}
}

// TestRunCancellationLatchesQuitBeforeRegistration pins the quit latch: a
// quit confirmed before a run registers its cancel function — during scope
// resolution or context assembly — still stops that run, so the quit is
// never lost. See TheoryOfTUIExit.
func TestRunCancellationLatchesQuitBeforeRegistration(t *testing.T) {
	var handle runCancellation
	handle.cancelRun()
	cancelled := false
	handle.set(func() { cancelled = true })
	if !cancelled {
		t.Fatal("expected the run registered after the quit to be cancelled at once")
	}
}

// TestForkTUIDisplaySkipsPhaseErrorRecoveryOnQuit pins the exit
// contract's recovery suppression: a cancelled run must not run the
// phase-error hook, because the hook asks for a handoff model request
// that condenses the interrupted output, and the user ended the session.
// See TheoryOfTUIExit and pipeline.TheoryOfRunCancellation.
func TestForkTUIDisplaySkipsPhaseErrorRecoveryOnQuit(t *testing.T) {
	defer func() { tuiRunCancel = runCancellation{} }()
	tui := newTUIForTest()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)
	run := scope.Get[pipeline.Run]()
	entered := make(chan struct{}, 1)
	hookCalled := false
	var runErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		var result pipeline.Result
		for _, e := range run(context.Background(), pipeline.RunOptions{
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(_ generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
					select {
					case entered <- struct{}{}:
					default:
					}
					newState, err := state.AppendContent(&generators.Content{
						Role:  generators.RoleModel,
						Parts: []generators.Part{generators.Text("partial output")},
					})
					if err != nil {
						return nil, state, err
					}
					<-ctx.Done()
					return nil, newState, ctx.Err()
				}
			},
			OnPhaseError: func(state generators.State, err error) generators.State {
				hookCalled = true
				return state
			},
		}, &result) {
			if e != nil {
				runErr = e
			}
		}
	}()
	<-entered
	tuiRunCancel.cancelRun()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the cancelled run")
	}
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("expected the cancellation error, got %v", runErr)
	}
	if hookCalled {
		t.Fatal("expected the phase error hook to be skipped after the quit")
	}
}
