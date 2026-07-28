package loops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/phases"
)

// appendPhase creates a phase that appends text content and returns nil
// (end of phase chain).
func appendPhase(text string) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		newState, err := state.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			return nil, state, err
		}
		return nil, newState, nil
	}
}

// errorPhase creates a phase that returns an error.
func errorPhase(err error) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		return nil, state, err
	}
}

func TestRunSingleRound(t *testing.T) {
	module := Module{}
	run := module.Run()

	result, err := run(context.Background(), RunOptions{
		Generator:    nil,
		InitialState: generators.NewPrompts("", nil),
		Components:   nil,
		PhaseBuilder: func(g generators.Generator) phases.Phase {
			return appendPhase("hello world")
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var foundText bool
	for c := range result.FinalState.Contents() {
		for _, p := range c.Parts {
			if text, ok := p.(generators.Text); ok && strings.Contains(string(text), "hello world") {
				foundText = true
			}
		}
	}
	if !foundText {
		t.Fatal("expected text content in final state")
	}
}

func TestRunMultiRoundTriggered(t *testing.T) {
	module := Module{}
	run := module.Run()

	callCount := 0
	phaseBuilder := func(g generators.Generator) phases.Phase {
		callCount++
		if callCount == 1 {
			return appendPhase(":::徕珑 <shell>\necho hello\n:::徕珑 </shell>\n")
		}
		return appendPhase("done")
	}

	comps := components.ComponentSet{
		{
			Kind: "shell",
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				return components.ProcessResult{
					Parts: []generators.Part{generators.Text("shell output")},
				}
			},
		},
	}

	result, err := run(context.Background(), RunOptions{
		Generator:    nil,
		InitialState: generators.NewPrompts("", nil),
		Components:   comps,
		PhaseBuilder: phaseBuilder,
		HTTPClient:   nets.HTTPClient{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 rounds, got %d", callCount)
	}

	var hasShellOutput bool
	var hasDone bool
	for c := range result.FinalState.Contents() {
		for _, p := range c.Parts {
			if text, ok := p.(generators.Text); ok {
				if strings.Contains(string(text), "shell output") {
					hasShellOutput = true
				}
				if strings.Contains(string(text), "done") {
					hasDone = true
				}
			}
		}
	}
	if !hasShellOutput {
		t.Fatal("expected shell output in state")
	}
	if !hasDone {
		t.Fatal("expected 'done' text in state")
	}
}

func TestRunBlockHandlerConsumed(t *testing.T) {
	module := Module{}
	run := module.Run()

	var consumedBlocks []blocks.Block
	blockHandler := func(block blocks.Block) (bool, error) {
		consumedBlocks = append(consumedBlocks, block)
		return true, nil
	}

	comps := components.ComponentSet{
		{
			Kind: "shell",
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				t.Fatal("component should not be called for consumed blocks")
				return components.ProcessResult{}
			},
		},
	}

	_, err := run(context.Background(), RunOptions{
		Generator:    nil,
		InitialState: generators.NewPrompts("", nil),
		Components:   comps,
		BlockHandler: blockHandler,
		PhaseBuilder: func(g generators.Generator) phases.Phase {
			return appendPhase(":::徕珑 <shell>\necho hi\n:::徕珑 </shell>\n")
		},
		HTTPClient: nets.HTTPClient{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(consumedBlocks) != 1 {
		t.Fatalf("expected 1 consumed block, got %d", len(consumedBlocks))
	}
	if consumedBlocks[0].Kind != "shell" {
		t.Fatalf("expected shell block, got %s", consumedBlocks[0].Kind)
	}
}

func TestRunPhaseError(t *testing.T) {
	module := Module{}
	run := module.Run()

	expectedErr := errors.New("phase failed")
	var onPhaseErrorCalled bool
	onPhaseError := func(state generators.State, err error) generators.State {
		onPhaseErrorCalled = true
		if err != expectedErr {
			t.Fatalf("expected %v, got %v", expectedErr, err)
		}
		return state
	}

	_, err := run(context.Background(), RunOptions{
		Generator:    nil,
		InitialState: generators.NewPrompts("", nil),
		Components:   nil,
		PhaseBuilder: func(g generators.Generator) phases.Phase {
			return errorPhase(expectedErr)
		},
		OnPhaseError: onPhaseError,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
	if !onPhaseErrorCalled {
		t.Fatal("OnPhaseError should be called")
	}
}

func TestRunRetryOnMissingCompletion(t *testing.T) {
	module := Module{}
	run := module.Run()

	callCount := 0
	phaseBuilder := func(g generators.Generator) phases.Phase {
		callCount++
		if callCount == 1 {
			// No completion block — should trigger retry.
			return appendPhase("incomplete output without summary")
		}
		// Second call includes a summary block.
		return appendPhase(":::徕珑 <summary>\nDone.\n:::徕珑 </summary>\n")
	}

	_, err := run(context.Background(), RunOptions{
		Generator:                nil,
		InitialState:             generators.NewPrompts("", nil),
		Components:               nil,
		PhaseBuilder:             phaseBuilder,
		RetryOnMissingCompletion: true,
		MaxRetries:               3,
		SummarizeIncomplete: func(text string) (string, error) {
			return "summary of incomplete", nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls (retry once), got %d", callCount)
	}
}

func TestRunRetryMaxRetries(t *testing.T) {
	module := Module{}
	run := module.Run()

	callCount := 0
	phaseBuilder := func(g generators.Generator) phases.Phase {
		callCount++
		return appendPhase("always incomplete")
	}

	_, err := run(context.Background(), RunOptions{
		Generator:                nil,
		InitialState:             generators.NewPrompts("", nil),
		Components:               nil,
		PhaseBuilder:             phaseBuilder,
		RetryOnMissingCompletion: true,
		MaxRetries:               2,
		SummarizeIncomplete: func(text string) (string, error) {
			return "summary", nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// maxRetries=2 means: initial + 2 retries = 3 calls.
	if callCount != 3 {
		t.Fatalf("expected 3 calls (initial + 2 retries), got %d", callCount)
	}
}

func TestRunOnRoundStartCalled(t *testing.T) {
	module := Module{}
	run := module.Run()

	callCount := 0
	onRoundStart := func() {
		callCount++
	}

	comps := components.ComponentSet{
		{
			Kind: "shell",
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				return components.ProcessResult{
					Parts: []generators.Part{generators.Text("output")},
				}
			},
		},
	}

	round := 0
	_, err := run(context.Background(), RunOptions{
		Generator:    nil,
		InitialState: generators.NewPrompts("", nil),
		Components:   comps,
		OnRoundStart: onRoundStart,
		PhaseBuilder: func(g generators.Generator) phases.Phase {
			round++
			if round == 1 {
				return appendPhase(":::徕珑 <shell>\necho hi\n:::徕珑 </shell>\n")
			}
			return appendPhase(":::徕珑 <summary>\nDone.\n:::徕珑 </summary>\n")
		},
		HTTPClient: nets.HTTPClient{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount < 2 {
		t.Fatalf("expected OnRoundStart called at least 2 times, got %d", callCount)
	}
}

func TestRunOnRoundSuccessCalled(t *testing.T) {
	module := Module{}
	run := module.Run()

	var successStates []generators.State
	var successSummaries [][]string

	onRoundSuccess := func(state generators.State, summaries []string) error {
		successStates = append(successStates, state)
		successSummaries = append(successSummaries, summaries)
		return nil
	}

	_, err := run(context.Background(), RunOptions{
		Generator:      nil,
		InitialState:   generators.NewPrompts("", nil),
		Components:     nil,
		OnRoundSuccess: onRoundSuccess,
		PhaseBuilder: func(g generators.Generator) phases.Phase {
			return appendPhase(":::徕珑 <summary>\nRound 1 done.\n:::徕珑 </summary>\n")
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(successStates) != 1 {
		t.Fatalf("expected 1 OnRoundSuccess call, got %d", len(successStates))
	}
	if len(successSummaries) != 1 || len(successSummaries[0]) != 1 {
		t.Fatalf("expected 1 summary, got %v", successSummaries)
	}
	if !strings.Contains(successSummaries[0][0], "Round 1 done") {
		t.Fatalf("unexpected summary: %s", successSummaries[0][0])
	}
}

func TestRunOnRoundSuccessError(t *testing.T) {
	module := Module{}
	run := module.Run()

	expectedErr := errors.New("flush failed")
	onRoundSuccess := func(state generators.State, summaries []string) error {
		return expectedErr
	}

	_, err := run(context.Background(), RunOptions{
		Generator:      nil,
		InitialState:   generators.NewPrompts("", nil),
		Components:     nil,
		OnRoundSuccess: onRoundSuccess,
		PhaseBuilder: func(g generators.Generator) phases.Phase {
			return appendPhase("hello")
		},
	})
	if err == nil {
		t.Fatal("expected error from OnRoundSuccess")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
}

func TestRunEmptyComponentsSingleShot(t *testing.T) {
	module := Module{}
	run := module.Run()

	phaseCalled := false
	result, err := run(context.Background(), RunOptions{
		Generator:    nil,
		InitialState: generators.NewPrompts("", nil),
		Components:   nil,
		PhaseBuilder: func(g generators.Generator) phases.Phase {
			phaseCalled = true
			return appendPhase("single shot")
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !phaseCalled {
		t.Fatal("phase should have been called")
	}
	// Single-shot mode: loop runs once and returns.
	if result.FinalState == nil {
		t.Fatal("expected non-nil final state")
	}
}

func TestRunMaxRounds(t *testing.T) {
	module := Module{}
	run := module.Run()

	callCount := 0
	comps := components.ComponentSet{
		{
			Kind: "shell",
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				return components.ProcessResult{
					Parts: []generators.Part{generators.Text("output")},
				}
			},
		},
	}

	_, err := run(context.Background(), RunOptions{
		Generator:    nil,
		InitialState: generators.NewPrompts("", nil),
		Components:   comps,
		MaxRounds:    3,
		PhaseBuilder: func(g generators.Generator) phases.Phase {
			callCount++
			return appendPhase(":::徕珑 <shell>\necho hi\n:::徕珑 </shell>\n")
		},
		HTTPClient: nets.HTTPClient{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Fatalf("expected 3 rounds (MaxRounds), got %d", callCount)
	}
}

func TestRunPhaseErrorNilStateFallback(t *testing.T) {
	// Reproduction: when a phase returns nil state on error,
	// loops.Run must fall back to the pre-phase state so OnPhaseError
	// receives a valid (non-nil) state. Before the fix, the nil state
	// was passed directly to OnPhaseError, causing a nil pointer
	// dereference when it called errState.AppendContent.
	module := Module{}
	run := module.Run()

	var onPhaseErrorState generators.State
	onPhaseError := func(state generators.State, err error) generators.State {
		onPhaseErrorState = state
		return state
	}

	initialState := generators.NewPrompts("", nil)
	_, err := run(context.Background(), RunOptions{
		Generator:    nil,
		InitialState: initialState,
		Components:   nil,
		PhaseBuilder: func(g generators.Generator) phases.Phase {
			return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
				return nil, nil, errors.New("generate failed")
			}
		},
		OnPhaseError: onPhaseError,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if onPhaseErrorState == nil {
		t.Fatal("OnPhaseError should receive a non-nil state, got nil")
	}
}
