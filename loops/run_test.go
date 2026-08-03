package loops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/phases"
)

// withRun creates a dscope scope for testing the loops package and calls
// fn with the resolved Run function. This follows the pattern established
// in anytexts.TestContextPrompt: construct a real dscope instance and use
// Call to obtain dscope-provided dependencies.
func withRun(t *testing.T, fn func(Run)) {
	t.Helper()
	loader := configs.NewLoader(nil, configs.LoaderConfig{})
	dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	).Call(func(run Run) {
		fn(run)
	})
}

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

// appendPhaseWithFinish creates a phase that appends text content and a
// finish reason, then returns nil (end of phase chain). Used to test
// retry behavior for abnormal finish reasons like "length".
func appendPhaseWithFinish(text string, finishReason string) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		newState, err := state.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			return nil, state, err
		}
		newState, err = newState.AppendContent(&generators.Content{
			Role: generators.RoleLog,
			Parts: []generators.Part{
				generators.FinishReason(finishReason),
			},
		})
		if err != nil {
			return nil, state, err
		}
		return nil, newState, nil
	}
}

// appendThenErrorPhase creates a phase that appends text content, then
// returns the given error. Used to test error retry when content has been
// output before the error occurs.
func appendThenErrorPhase(text string, err error) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		newState, appendErr := state.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if appendErr != nil {
			return nil, state, appendErr
		}
		return nil, newState, err
	}
}

// appendPhaseWithFlush appends text content and then flushes the state,
// so ParserState.Flush collects parse errors from unclosed blocks. Used
// to test parse-error correction in the loop. See
// TheoryOfParseErrorCollection.
func appendPhaseWithFlush(text string) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		newState, err := state.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			return nil, state, err
		}
		newState, err = newState.Flush()
		if err != nil {
			return nil, state, err
		}
		return nil, newState, nil
	}
}

func TestRunParseErrorCorrection(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				// Emit an unclosed block (no closing delimiter) — a
				// parse error that must be fed back for self-correction.
				return appendPhaseWithFlush("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")
			}
			// Second round: corrected output with a summary block.
			return appendPhaseWithFlush("<<徕珑 <summary>\nDone.\n徕珑\n")
		}

		result, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: phaseBuilder,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 rounds (1 parse-error correction round), got %d", callCount)
		}

		// The parse error feedback must be present in the state as user
		// content so the model can correct the malformed block.
		foundFeedback := false
		for c := range result.FinalState.Contents() {
			if c.Role == generators.RoleUser {
				for _, p := range c.Parts {
					if text, ok := p.(generators.Text); ok {
						if strings.Contains(string(text), "could not be parsed") {
							foundFeedback = true
						}
					}
				}
			}
		}
		if !foundFeedback {
			t.Fatal("expected parse error feedback in state")
		}
	})
}

func TestRunParseErrorCorrectionBound(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			// Persistently emit an unclosed block — a parse error every
			// round. The correction loop must stop after
			// maxParseErrorRounds.
			return appendPhaseWithFlush("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")
		}

		_, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: phaseBuilder,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Initial round + maxParseErrorRounds correction rounds.
		if callCount != 1+maxParseErrorRounds {
			t.Fatalf("expected %d rounds, got %d", 1+maxParseErrorRounds, callCount)
		}
	})
}

func TestRunParseErrorCorrectionWithComponents(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				// A complete shell block followed by an unclosed change
				// block. The shell block is processed by the component;
				// the parse error feedback is prepended to the shell
				// output.
				return appendPhaseWithFlush("<<龘靐 <shell>\necho hi\n龘靐\n<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")
			}
			return appendPhaseWithFlush("<<徕珑 <summary>\nDone.\n徕珑\n")
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

		// Both the parse error feedback and the shell output must be in
		// the user content of the final state.
		var userText string
		for c := range result.FinalState.Contents() {
			if c.Role == generators.RoleUser {
				for _, p := range c.Parts {
					if text, ok := p.(generators.Text); ok {
						userText += string(text)
					}
				}
			}
		}
		if !strings.Contains(userText, "could not be parsed") {
			t.Fatal("expected parse error feedback in user content")
		}
		if !strings.Contains(userText, "shell output") {
			t.Fatal("expected shell output in user content")
		}
	})
}

// errorPhase creates a phase that returns an error.
func errorPhase(err error) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		return nil, state, err
	}
}

func TestRunSingleRound(t *testing.T) {
	withRun(t, func(run Run) {
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
	})
}

func TestRunMultiRoundTriggered(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("<<徕珑 <shell>\necho hello\n徕珑\n")
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
	})
}

func TestRunBlockHandlerConsumed(t *testing.T) {
	withRun(t, func(run Run) {
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
				return appendPhase("<<徕珑 <shell>\necho hi\n徕珑\n")
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
	})
}

func TestRunPhaseError(t *testing.T) {
	withRun(t, func(run Run) {
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
	})
}

func TestRunRetryOnMissingCompletion(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				// No completion block — should trigger retry.
				return appendPhase("incomplete output without summary")
			}
			// Second call includes a summary block.
			return appendPhase("<<徕珑 <summary>\nDone.\n徕珑\n")
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
	})
}

func TestRunRetryOnAbnormalFinishReason(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				// Summary block present but finish reason is "length"
				// (max-token truncation). This should trigger retry
				// despite the summary block.
				return appendPhaseWithFinish("<<徕珑 <summary>\nDone.\n徕珑\n", "length")
			}
			// Second call: normal finish reason with summary.
			return appendPhaseWithFinish("<<徕珑 <summary>\nDone.\n徕珑\n", "stop")
		}

		_, err := run(context.Background(), RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			SummarizeIncomplete: func(text string) (string, error) {
				return "summary of truncated output", nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 calls (retry once for abnormal finish reason), got %d", callCount)
		}
	})
}

func TestRunNoRetryOnNormalFinishReason(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			return appendPhaseWithFinish("<<徕珑 <summary>\nDone.\n徕珑\n", "stop")
		}

		_, err := run(context.Background(), RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			SummarizeIncomplete: func(text string) (string, error) {
				return "summary", nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 call (normal finish reason, no retry), got %d", callCount)
		}
	})
}

func TestRunRetryMaxRetries(t *testing.T) {
	withRun(t, func(run Run) {
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
	})
}

func TestRunOnRoundStartCalled(t *testing.T) {
	withRun(t, func(run Run) {
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
					return appendPhase("<<徕珑 <shell>\necho hi\n徕珑\n")
				}
				return appendPhase("<<徕珑 <summary>\nDone.\n徕珑\n")
			},
			HTTPClient: nets.HTTPClient{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount < 2 {
			t.Fatalf("expected OnRoundStart called at least 2 times, got %d", callCount)
		}
	})
}

func TestRunOnRoundSuccessCalled(t *testing.T) {
	withRun(t, func(run Run) {
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
				return appendPhase("<<徕珑 <summary>\nRound 1 done.\n徕珑\n")
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
	})
}

func TestRunOnRoundSuccessError(t *testing.T) {
	withRun(t, func(run Run) {
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
	})
}

func TestRunEmptyComponentsSingleShot(t *testing.T) {
	withRun(t, func(run Run) {
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
	})
}

func TestRunMaxRounds(t *testing.T) {
	withRun(t, func(run Run) {
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
				return appendPhase("<<徕珑 <shell>\necho hi\n徕珑\n")
			},
			HTTPClient: nets.HTTPClient{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 3 {
			t.Fatalf("expected 3 rounds (MaxRounds), got %d", callCount)
		}
	})
}

func TestRunPhaseErrorNilStateFallback(t *testing.T) {
	withRun(t, func(run Run) {
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
	})
}

func TestRunRetryOnErrorWithContent(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				return appendThenErrorPhase("partial model output", errors.New("something went wrong"))
			}
			return appendPhase("<<徕珑 <summary>\nDone.\n徕珑\n")
		}

		result, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: phaseBuilder,
			RetryOnError: true,
			MaxRetries:   3,
			SummarizeIncomplete: func(text string) (string, error) {
				return "summary of partial output", nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 calls (retry once), got %d", callCount)
		}

		foundErrorMsg := false
		foundSummary := false
		for c := range result.FinalState.Contents() {
			if c.Role == generators.RoleUser {
				for _, p := range c.Parts {
					if text, ok := p.(generators.Text); ok {
						if strings.Contains(string(text), "error occurred") {
							foundErrorMsg = true
						}
						if strings.Contains(string(text), "summary of partial output") {
							foundSummary = true
						}
					}
				}
			}
		}
		if !foundErrorMsg {
			t.Fatal("expected error message to be appended as user content")
		}
		if !foundSummary {
			t.Fatal("expected incomplete output summary to be appended as user content")
		}
	})
}

func TestRunRetryOnErrorMaxRetries(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			return appendThenErrorPhase("some output", errors.New("always fails"))
		}

		_, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: phaseBuilder,
			OnRoundStart: func() {},
			RetryOnError: true,
			MaxRetries:   2,
		})
		if err == nil {
			t.Fatal("expected error after max retries exhausted")
		}
		if callCount != 3 {
			t.Fatalf("expected 3 calls (initial + 2 retries), got %d", callCount)
		}
	})
}

func TestRunOnErrorNoRetryWhenDisabled(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			return appendThenErrorPhase("some content", errors.New("some error"))
		}

		_, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: phaseBuilder,
			RetryOnError: false,
		})
		if err == nil {
			t.Fatal("expected error when RetryOnError is disabled")
		}
		if callCount != 1 {
			t.Fatalf("expected 1 call (no retry), got %d", callCount)
		}
	})
}

func TestRunRetryOnErrorNoContentNoRetry(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			return errorPhase(errors.New("system error"))
		}

		_, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: phaseBuilder,
			RetryOnError: true,
			MaxRetries:   3,
		})
		if err == nil {
			t.Fatal("expected error without content output")
		}
		if callCount != 1 {
			t.Fatalf("expected 1 call (no content output, no retry), got %d", callCount)
		}
	})
}

func TestRunOnIdleCalledWhenNoComponentTriggers(t *testing.T) {
	withRun(t, func(run Run) {
		genCount := 0
		idleCount := 0

		phaseBuilder := func(g generators.Generator) phases.Phase {
			genCount++
			return appendPhase("model output")
		}

		comps := components.ComponentSet{
			{
				Kind: "shell",
				Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
					return components.ProcessResult{}
				},
			},
		}

		onIdle := phases.IdleHandler(func(ctx context.Context, state generators.State) (generators.State, bool, error) {
			idleCount++
			if idleCount <= 2 {
				state, _ = state.AppendContent(&generators.Content{
					Role:  generators.RoleUser,
					Parts: []generators.Part{generators.Text("user input")},
				})
				return state, true, nil
			}
			return state, false, nil
		})

		_, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			PhaseBuilder: phaseBuilder,
			OnIdle:       onIdle,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if genCount != 3 {
			t.Fatalf("expected 3 generate calls, got %d", genCount)
		}
		if idleCount != 3 {
			t.Fatalf("expected 3 idle calls, got %d", idleCount)
		}
	})
}

func TestRunOnIdleNotCalledWhenComponentTriggers(t *testing.T) {
	withRun(t, func(run Run) {
		genCount := 0
		idleCount := 0

		phaseBuilder := func(g generators.Generator) phases.Phase {
			genCount++
			return appendPhase("<<徕珑 <shell>\necho hi\n徕珑\n")
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

		onIdle := phases.IdleHandler(func(ctx context.Context, state generators.State) (generators.State, bool, error) {
			idleCount++
			return state, false, nil
		})

		_, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			PhaseBuilder: phaseBuilder,
			OnIdle:       onIdle,
			MaxRounds:    3,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if genCount != 3 {
			t.Fatalf("expected 3 generate calls, got %d", genCount)
		}
		if idleCount != 0 {
			t.Fatalf("expected 0 idle calls (component always triggers), got %d", idleCount)
		}
	})
}

func TestRunOnIdleError(t *testing.T) {
	withRun(t, func(run Run) {
		expectedErr := errors.New("idle error")
		onIdle := phases.IdleHandler(func(ctx context.Context, state generators.State) (generators.State, bool, error) {
			return state, false, expectedErr
		})

		comps := components.ComponentSet{
			{
				Kind: "shell",
				Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
					return components.ProcessResult{}
				},
			},
		}

		_, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return appendPhase("model output")
			},
			OnIdle: onIdle,
		})
		if err == nil {
			t.Fatal("expected error from OnIdle")
		}
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestRunOnIdleNilNoEffect(t *testing.T) {
	withRun(t, func(run Run) {
		genCount := 0

		phaseBuilder := func(g generators.Generator) phases.Phase {
			genCount++
			return appendPhase("model output")
		}

		comps := components.ComponentSet{
			{
				Kind: "shell",
				Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
					return components.ProcessResult{}
				},
			},
		}

		_, err := run(context.Background(), RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			PhaseBuilder: phaseBuilder,
			OnIdle:       nil,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if genCount != 1 {
			t.Fatalf("expected 1 generate call (no OnIdle to continue), got %d", genCount)
		}
	})
}

func TestRunRemainingBlocksAccumulateAcrossRounds(t *testing.T) {
	// When a round emits an unmatched block (done) and another component
	// triggers a new round, the unmatched block must survive into the
	// final Result.RemainingBlocks.
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("<<徕珑 <done>\ngoal achieved\n徕珑\n<<龘靐 <other>\ntrigger\n龘靐\n")
			}
			return appendPhase("<<徕珑 <summary>\nDone.\n徕珑\n")
		}

		comps := components.ComponentSet{
			{
				Kind: "other",
				Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
					return components.ProcessResult{
						Parts: []generators.Part{generators.Text("trigger output")},
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

		foundDone := false
		for _, block := range result.RemainingBlocks {
			if block.Kind == "done" {
				foundDone = true
				break
			}
		}
		if !foundDone {
			t.Fatal("done block from an earlier round must be preserved in Result.RemainingBlocks")
		}
	})
}
