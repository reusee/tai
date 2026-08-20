package loops

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"log/slog"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/phases"
)

func withRun(t *testing.T, fn func(Run)) {
	t.Helper()
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Call(func(run Run) {
		fn(run)
	})
}

// runOnce runs the loop to completion and returns the result and the
// terminal error, if any. It adapts the iterator-based Run to the
// (Result, error) shape used by the tests.
func runOnce(run Run, opts RunOptions) (Result, error) {
	var result Result
	for e := range run(context.Background(), opts, &result) {
		return result, e
	}
	return result, nil
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

// appendPhaseWithUsage creates a phase that appends text content and a
// token usage part, then returns nil (end of phase chain). Used to test
// the round usage log record. See TheoryOfUsageLogging.
func appendPhaseWithUsage(text string, usage generators.Usage) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		newState, err := state.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			return nil, state, err
		}
		newState, err = newState.AppendContent(&generators.Content{
			Role:  generators.RoleLog,
			Parts: []generators.Part{usage},
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
				return appendPhaseWithFlush("<<龘靐 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")
			}
			// Second round: corrected output with a summary block.
			return appendPhaseWithFlush("<<龘靐 <summary>\nDone.\n龘靐\n")
		}

		result, err := runOnce(run, RunOptions{
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
			return appendPhaseWithFlush("<<龘靐 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")
		}

		_, err := runOnce(run, RunOptions{
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
				return appendPhaseWithFlush("<<齉爩 <shell>\necho hi\n齉爩\n<<龘靐 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")
			}
			return appendPhaseWithFlush("<<龘靐 <summary>\nDone.\n龘靐\n")
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

		result, err := runOnce(run, RunOptions{
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

func TestRunParseErrorCorrectionCumulativeBound(t *testing.T) {
	// When components keep triggering rounds, a model that persistently
	// emits malformed blocks must not restart the parse-error correction
	// cycle after the budget is exhausted. The correction budget is
	// cumulative per run: feedback is given only for the first
	// maxParseErrorRounds rounds with parse errors, then stops until a
	// clean round resets the budget. Uncorrected parse errors are
	// surfaced in Result.ParseErrors. See TheoryOfLoops.
	withRun(t, func(run Run) {
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

		// Every round emits a complete shell block (triggers a new round)
		// plus an unclosed change block (parse error).
		phaseBuilder := func(g generators.Generator) phases.Phase {
			return appendPhaseWithFlush(
				"<<齉爩 <shell>\necho hi\n齉爩\n" +
					"<<龘靐 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")
		}

		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			PhaseBuilder: phaseBuilder,
			HTTPClient:   nets.HTTPClient{},
			MaxRounds:    8,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Parse error feedback appears only in the first maxParseErrorRounds
		// rounds. Rounds 4+ receive only shell output.
		feedbackCount := 0
		for c := range result.FinalState.Contents() {
			if c.Role != generators.RoleUser {
				continue
			}
			for _, p := range c.Parts {
				if text, ok := p.(generators.Text); ok {
					if strings.Contains(string(text), "could not be parsed") {
						feedbackCount++
					}
				}
			}
		}
		if feedbackCount != maxParseErrorRounds {
			t.Fatalf("expected %d parse-error feedbacks (cumulative bound), got %d", maxParseErrorRounds, feedbackCount)
		}

		// The uncorrected parse errors must be surfaced in the result.
		if len(result.ParseErrors) == 0 {
			t.Fatal("expected uncorrected parse errors in Result.ParseErrors")
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
		result, err := runOnce(run, RunOptions{
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
				return appendPhase("<<龘靐 <shell>\necho hello\n龘靐\n")
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

		result, err := runOnce(run, RunOptions{
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

		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			BlockHandler: blockHandler,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return appendPhase("<<龘靐 <shell>\necho hi\n龘靐\n")
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

		_, err := runOnce(run, RunOptions{
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
			return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
		}

		_, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary of incomplete", Prompt: "retry prompt"}, nil
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

func TestRunRetryExhaustedAppendsSummaryBlock(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		var successSummaries [][]string
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			return appendPhase("always incomplete")
		}

		result, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               1,
			OnRoundSuccess: func(state generators.State, summaries []string) error {
				successSummaries = append(successSummaries, summaries)
				return nil
			},
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "synthesized summary", Prompt: "retry prompt"}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 calls (initial + 1 retry), got %d", callCount)
		}
		if len(successSummaries) != 1 || len(successSummaries[0]) != 1 ||
			successSummaries[0][0] != "synthesized summary" {
			t.Fatalf("expected the synthesized summary in OnRoundSuccess, got %v", successSummaries)
		}

		// The exhausted round must have a synthesized summary block
		// appended to the state so the TUI's Round tab can display it.
		foundSummary := false
		for c := range result.FinalState.Contents() {
			for _, p := range c.Parts {
				if text, ok := p.(generators.Text); ok {
					if strings.Contains(string(text), "<summary>") &&
						strings.Contains(string(text), "synthesized summary") {
						foundSummary = true
					}
				}
			}
		}
		if !foundSummary {
			t.Fatal("expected a synthesized summary block in the final state")
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
				return appendPhaseWithFinish("<<龘靐 <summary>\nDone.\n龘靐\n", "length")
			}
			// Second call: normal finish reason with summary.
			return appendPhaseWithFinish("<<龘靐 <summary>\nDone.\n龘靐\n", "stop")
		}

		_, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary of truncated output", Prompt: "retry prompt"}, nil
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
			return appendPhaseWithFinish("<<龘靐 <summary>\nDone.\n龘靐\n", "stop")
		}

		_, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
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

		_, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               2,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
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
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			OnRoundStart: onRoundStart,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				round++
				if round == 1 {
					return appendPhase("<<龘靐 <shell>\necho hi\n龘靐\n")
				}
				return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
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

		_, err := runOnce(run, RunOptions{
			Generator:      nil,
			InitialState:   generators.NewPrompts("", nil),
			Components:     nil,
			OnRoundSuccess: onRoundSuccess,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return appendPhase("<<龘靐 <summary>\nRound 1 done.\n龘靐\n")
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

func TestRunLogsRoundUsage(t *testing.T) {
	// The Run loop must record the aggregated token usage of each round
	// to the logger, so token consumption is visible in log output and in
	// the TUI's Logs pane, not only in the end-of-session statistics
	// table. The logger is forked directly so the test controls the
	// output sink; forking the logs.Writer would be ignored when the
	// logger provider detects a systemd service (which creates only a
	// journal handler). See TheoryOfUsageLogging.
	var buf bytes.Buffer
	logger := logs.Logger{slog.New(slog.NewTextHandler(&buf, nil))}
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() logs.Logger { return logger },
	).Call(func(run Run) {
		usage := generators.Usage{}
		usage.Prompt.TokenCount = 100
		usage.Prompt.TokenCountCached = 20
		usage.Candidates.TokenCount = 50
		usage.Thoughts.TokenCount = 10

		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return appendPhaseWithUsage("model output", usage)
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		for _, want := range []string{
			"msg=usage",
			"round=1",
			"prompt=100",
			"cached=20",
			"completion=50",
			"thoughts=10",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("expected %q in log output, got: %s", want, output)
			}
		}
	})
}

func TestRunLogsRoundUsageMultipleUsageParts(t *testing.T) {
	// If a generator emits multiple Usage parts during streaming (e.g. Gemini),
	// logRoundUsage must take the final Usage snapshot rather than summing them.
	// The logger is forked directly so the test controls the output sink;
	// forking the logs.Writer would be ignored when the logger provider
	// detects a systemd service. See TheoryOfUsageLogging.
	var buf bytes.Buffer
	logger := logs.Logger{slog.New(slog.NewTextHandler(&buf, nil))}
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() logs.Logger { return logger },
	).Call(func(run Run) {
		usage1 := generators.Usage{}
		usage1.Prompt.TokenCount = 100
		usage1.Candidates.TokenCount = 10

		usage2 := generators.Usage{}
		usage2.Prompt.TokenCount = 100
		usage2.Candidates.TokenCount = 50

		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					s, err := state.AppendContent(&generators.Content{
						Role:  generators.RoleLog,
						Parts: []generators.Part{usage1},
					})
					if err != nil {
						return nil, state, err
					}
					s, err = s.AppendContent(&generators.Content{
						Role:  generators.RoleAssistant,
						Parts: []generators.Part{generators.Text("output")},
					})
					if err != nil {
						return nil, state, err
					}
					s, err = s.AppendContent(&generators.Content{
						Role:  generators.RoleLog,
						Parts: []generators.Part{usage2},
					})
					if err != nil {
						return nil, state, err
					}
					return nil, s, nil
				}
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "prompt=100") {
			t.Fatalf("expected prompt=100 (final snapshot, not 200 sum), got: %s", output)
		}
		if !strings.Contains(output, "completion=50") {
			t.Fatalf("expected completion=50 (final snapshot, not 60 sum), got: %s", output)
		}
	})
}

func TestRunOnRoundSuccessError(t *testing.T) {
	withRun(t, func(run Run) {
		expectedErr := errors.New("flush failed")
		onRoundSuccess := func(state generators.State, summaries []string) error {
			return expectedErr
		}

		_, err := runOnce(run, RunOptions{
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
		result, err := runOnce(run, RunOptions{
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

		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   comps,
			MaxRounds:    3,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				callCount++
				return appendPhase("<<龘靐 <shell>\necho hi\n龘靐\n")
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
		_, err := runOnce(run, RunOptions{
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
			return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
		}

		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: phaseBuilder,
			RetryOnError: true,
			MaxRetries:   3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary of partial output", Prompt: "retry prompt content"}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 calls (retry once), got %d", callCount)
		}

		foundError := false
		foundRetryPrompt := false
		for c := range result.FinalState.Contents() {
			if c.Role == generators.RoleUser {
				for _, p := range c.Parts {
					if text, ok := p.(generators.Text); ok {
						if strings.Contains(string(text), "error occurred") {
							foundError = true
						}
						if strings.Contains(string(text), "retry prompt content") {
							foundRetryPrompt = true
						}
					}
				}
			}
		}
		if !foundError {
			t.Fatal("expected error message in state")
		}
		if !foundRetryPrompt {
			t.Fatal("expected retry prompt in state")
		}
	})
}

func TestRunRetryOnApplyErrorGuidance(t *testing.T) {
	// Change block apply errors must produce specific guidance in the
	// retry feedback: the retry discards all change blocks from the
	// failed attempt, so the model must re-emit every intended change
	// block. Generic error feedback would leave the model guessing
	// whether its changes were accepted. See TheoryOfLoops.
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				return appendThenErrorPhase(
					"partial model output",
					&changes.ApplyError{Err: errors.New("apply change block MODIFY Foo: target not found")},
				)
			}
			return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
		}

		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: phaseBuilder,
			RetryOnError: true,
			MaxRetries:   3,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 calls (retry once), got %d", callCount)
		}

		foundGuidance := false
		for c := range result.FinalState.Contents() {
			if c.Role != generators.RoleUser {
				continue
			}
			for _, p := range c.Parts {
				if text, ok := p.(generators.Text); ok {
					// The guidance phrase starts the sentence ("Re-emit"),
					// so compare case-insensitively to the lowercase
					// assertion. See TheoryOfLoops.
					if strings.Contains(strings.ToLower(string(text)), "re-emit every intended change block") {
						foundGuidance = true
					}
				}
			}
		}
		if !foundGuidance {
			t.Fatal("expected ApplyError guidance in retry feedback")
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

		_, err := runOnce(run, RunOptions{
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

func TestRunRetryFeedbackIncludesAttemptNumber(t *testing.T) {
	withRun(t, func(run Run) {
		t.Run("MissingCompletion", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) phases.Phase {
				callCount++
				if callCount == 1 {
					return appendPhase("incomplete output without summary")
				}
				return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
			}

			result, err := runOnce(run, RunOptions{
				Generator:                nil,
				InitialState:             generators.NewPrompts("", nil),
				Components:               nil,
				PhaseBuilder:             phaseBuilder,
				RetryOnMissingCompletion: true,
				MaxRetries:               1,
				Handoff: func(text string) (*Handoff, error) {
					return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
				},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if callCount != 2 {
				t.Fatalf("expected 2 calls, got %d", callCount)
			}

			foundAttempt := false
			for c := range result.FinalState.Contents() {
				if c.Role == generators.RoleUser {
					for _, p := range c.Parts {
						if text, ok := p.(generators.Text); ok {
							if strings.Contains(string(text), "retry attempt 1 of 1") {
								foundAttempt = true
							}
						}
					}
				}
			}
			if !foundAttempt {
				t.Fatal("expected retry attempt number in state")
			}
		})

		t.Run("Error", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) phases.Phase {
				callCount++
				if callCount == 1 {
					return appendThenErrorPhase("partial output", errors.New("some error"))
				}
				return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
			}

			result, err := runOnce(run, RunOptions{
				Generator:    nil,
				InitialState: generators.NewPrompts("", nil),
				Components:   nil,
				PhaseBuilder: phaseBuilder,
				RetryOnError: true,
				MaxRetries:   1,
				Handoff: func(text string) (*Handoff, error) {
					return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
				},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if callCount != 2 {
				t.Fatalf("expected 2 calls, got %d", callCount)
			}

			foundAttempt := false
			for c := range result.FinalState.Contents() {
				if c.Role == generators.RoleUser {
					for _, p := range c.Parts {
						if text, ok := p.(generators.Text); ok {
							if strings.Contains(string(text), "retry attempt 1 of 1") {
								foundAttempt = true
							}
						}
					}
				}
			}
			if !foundAttempt {
				t.Fatal("expected retry attempt number in state")
			}
		})
	})
}

func TestRunRetryFeedbackInstructsReEmittingBlocks(t *testing.T) {
	withRun(t, func(run Run) {
		t.Run("MissingCompletion", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) phases.Phase {
				callCount++
				if callCount == 1 {
					return appendPhase("incomplete output without summary")
				}
				return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
			}

			result, err := runOnce(run, RunOptions{
				Generator:                nil,
				InitialState:             generators.NewPrompts("", nil),
				Components:               nil,
				PhaseBuilder:             phaseBuilder,
				RetryOnMissingCompletion: true,
				MaxRetries:               1,
				Handoff: func(text string) (*Handoff, error) {
					return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
				},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if callCount != 2 {
				t.Fatalf("expected 2 calls, got %d", callCount)
			}

			foundInstruction := false
			for c := range result.FinalState.Contents() {
				if c.Role == generators.RoleUser {
					for _, p := range c.Parts {
						if text, ok := p.(generators.Text); ok {
							if strings.Contains(string(text), "Re-emit every block") {
								foundInstruction = true
							}
						}
					}
				}
			}
			if !foundInstruction {
				t.Fatal("expected re-emit instruction in state")
			}
		})

		t.Run("Error", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) phases.Phase {
				callCount++
				if callCount == 1 {
					return appendThenErrorPhase("partial output", errors.New("some error"))
				}
				return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
			}

			result, err := runOnce(run, RunOptions{
				Generator:    nil,
				InitialState: generators.NewPrompts("", nil),
				Components:   nil,
				PhaseBuilder: phaseBuilder,
				RetryOnError: true,
				MaxRetries:   1,
				Handoff: func(text string) (*Handoff, error) {
					return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
				},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if callCount != 2 {
				t.Fatalf("expected 2 calls, got %d", callCount)
			}

			foundInstruction := false
			for c := range result.FinalState.Contents() {
				if c.Role == generators.RoleUser {
					for _, p := range c.Parts {
						if text, ok := p.(generators.Text); ok {
							if strings.Contains(string(text), "Re-emit every block") {
								foundInstruction = true
							}
						}
					}
				}
			}
			if !foundInstruction {
				t.Fatal("expected re-emit instruction in state")
			}
		})
	})
}

func TestRunOnRoundTruncatedCalled(t *testing.T) {
	withRun(t, func(run Run) {
		var truncatedSummaries []string
		onRoundTruncated := func(truncatedState generators.State, retryBaseState generators.State, summary string) error {
			truncatedSummaries = append(truncatedSummaries, summary)
			return nil
		}

		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("incomplete output without summary")
			}
			return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
		}

		_, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			OnRoundTruncated:         onRoundTruncated,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "truncated summary", Prompt: "retry prompt"}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 calls, got %d", callCount)
		}
		if len(truncatedSummaries) != 1 {
			t.Fatalf("expected 1 OnRoundTruncated call, got %d", len(truncatedSummaries))
		}
		if truncatedSummaries[0] != "truncated summary" {
			t.Fatalf("expected 'truncated summary', got %q", truncatedSummaries[0])
		}
	})
}

func TestRunRetryPromptIsIncludedDirectly(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("incomplete output without summary")
			}
			return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
		}

		result, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary", Prompt: "compressed content"}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 calls, got %d", callCount)
		}

		foundContent := false
		for c := range result.FinalState.Contents() {
			if c.Role == generators.RoleUser {
				for _, p := range c.Parts {
					if text, ok := p.(generators.Text); ok {
						if strings.Contains(string(text), "compressed content") {
							foundContent = true
						}
					}
				}
			}
		}
		if !foundContent {
			t.Fatal("expected the retry prompt content in the user content")
		}
	})
}

func TestFormatHandoffPrompt(t *testing.T) {
	msg := formatHandoffPrompt("retry content", 1, 3)
	if !strings.Contains(msg, "retry attempt 1 of 3") {
		t.Fatalf("expected the retry attempt number, got: %s", msg)
	}
	if !strings.Contains(msg, "retry content") {
		t.Fatalf("expected the retry content, got: %s", msg)
	}
	// The retry prompt must state that nothing in the interrupted
	// attempt was completed on disk: changes are atomic, so the handoff carries
	// forward thinking, not work status. See states.TheoryOfHandoff.
	if !strings.Contains(msg, "no completed work") {
		t.Fatalf("expected the atomicity note in the retry prompt, got: %s", msg)
	}
	if !strings.Contains(msg, "no next step to carry forward") {
		t.Fatalf("expected the no-next-step note in the retry prompt, got: %s", msg)
	}
	// The retry prompt must instruct partitioning work with continue blocks
	// to prevent repeated truncation. See states.TheoryOfHandoff.
	if !strings.Contains(msg, "partition the work") {
		t.Fatalf("expected partitioning instruction in the retry prompt, got: %s", msg)
	}
	if !strings.Contains(msg, "continue block") {
		t.Fatalf("expected continue block guidance in the retry prompt, got: %s", msg)
	}
	// The retry prompt must not instruct re-reading the filesystem: the
	// context already carries the latest state. See states.TheoryOfHandoff.
	if strings.Contains(msg, "Re-read the current filesystem state") {
		t.Fatalf("the retry prompt must not instruct re-reading the filesystem, got: %s", msg)
	}
	if strings.Contains(msg, "<summary>") {
		t.Fatalf("expected no summary block in the retry prompt, got: %s", msg)
	}
	if strings.Contains(msg, "<continue>") {
		t.Fatalf("expected no continue block in the retry prompt, got: %s", msg)
	}
}

func TestRunOnErrorNoRetryWhenDisabled(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			return appendThenErrorPhase("some content", errors.New("some error"))
		}

		_, err := runOnce(run, RunOptions{
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

		_, err := runOnce(run, RunOptions{
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

		_, err := runOnce(run, RunOptions{
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
			return appendPhase("<<龘靐 <shell>\necho hi\n龘靐\n")
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

		_, err := runOnce(run, RunOptions{
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

		_, err := runOnce(run, RunOptions{
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

		_, err := runOnce(run, RunOptions{
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
				return appendPhase("<<龘靐 <done>\ngoal achieved\n龘靐\n<<齉爩 <other>\ntrigger\n齉爩\n")
			}
			return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
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

		result, err := runOnce(run, RunOptions{
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

func TestExtractIncompleteOutput(t *testing.T) {
	state := generators.NewPrompts("", []*generators.Content{
		{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("question")}},
		{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("base answer")}},
		{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("partial answer"), generators.Thought("thinking...")}},
	})

	t.Run("SkipBase", func(t *testing.T) {
		got := ExtractIncompleteOutput(state, 2)
		if !strings.Contains(got, "partial answer") || !strings.Contains(got, "thinking...") {
			t.Fatalf("expected partial answer and thoughts, got %q", got)
		}
		if strings.Contains(got, "base answer") {
			t.Fatalf("base answer should be skipped, got %q", got)
		}
	})

	t.Run("NoSkip", func(t *testing.T) {
		got := ExtractIncompleteOutput(state, 0)
		if !strings.Contains(got, "base answer") {
			t.Fatalf("expected base answer when no skip, got %q", got)
		}
		// ExtractIncompleteOutput processes all roles, so user content
		// is included when prevCount is 0.
		if !strings.Contains(got, "question") {
			t.Fatalf("expected question when no skip, got %q", got)
		}
	})

	t.Run("SkipAll", func(t *testing.T) {
		got := ExtractIncompleteOutput(state, 3)
		if got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	t.Run("NonTextPartsIgnored", func(t *testing.T) {
		stateWithMeta := generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleLog, Parts: []generators.Part{
				generators.Usage{},
				generators.FinishReason("stop"),
				generators.Error{Error: errors.New("err")},
			}},
			{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("visible")}},
		})
		got := ExtractIncompleteOutput(stateWithMeta, 0)
		if got != "visible" {
			t.Fatalf("expected only text parts, got %q", got)
		}
	})
}

func TestRunStateDecorators(t *testing.T) {
	withRun(t, func(run Run) {
		var applied []string
		var observed []string
		initialState := generators.NewPrompts("", nil)
		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: initialState,
			Components:   nil,
			StateDecorators: []StateDecorator{
				func(state generators.State) generators.State {
					applied = append(applied, "first")
					return state
				},
				func(state generators.State) generators.State {
					applied = append(applied, "second")
					return testObservingState{
						upstream: state,
						onAppend: func(content *generators.Content) {
							for _, part := range content.Parts {
								if text, ok := part.(generators.Text); ok {
									observed = append(observed, string(text))
								}
							}
						},
					}
				},
			},
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return appendPhase("hello")
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(applied) != 2 || applied[0] != "first" || applied[1] != "second" {
			t.Fatalf("expected decorators applied in order, got %v", applied)
		}
		if len(observed) != 1 || observed[0] != "hello" {
			t.Fatalf("expected decorator to observe content, got %v", observed)
		}
		// The final state is the decorator's wrapped state: the loop uses
		// the state returned by the last decorator.
		if _, ok := generators.As[testObservingState](result.FinalState); !ok {
			t.Fatal("expected final state to be the decorator's wrapped state")
		}
	})
}

func TestRunStateModificationTriggersRound(t *testing.T) {
	// A component that modifies State (like request-context) must trigger
	// a new generation round. When the model emits a component-triggering
	// block without a summary block, the round must NOT be retried for
	// missing completion — the model is waiting for component processing,
	// not truncated. See TheoryOfLoops and TheoryOfComponents.
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) phases.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("<<龘靐 <state-modifier>\nrequest\n龘靐\n")
			}
			return appendPhase("<<龘靐 <summary>\nDone.\n龘靐\n")
		}

		comps := components.ComponentSet{
			{
				Kind: "state-modifier",
				Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
					newState, err := pctx.State.AppendContent(&generators.Content{
						Role:  generators.RoleUser,
						Parts: []generators.Part{generators.Text("fetched context")},
					})
					if err != nil {
						return components.ProcessResult{Err: err}
					}
					return components.ProcessResult{State: newState}
				},
			},
		}

		result, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               comps,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 rounds (state modification triggers next, no retry for missing summary), got %d", callCount)
		}

		found := false
		for c := range result.FinalState.Contents() {
			for _, p := range c.Parts {
				if text, ok := p.(generators.Text); ok && string(text) == "fetched context" {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("expected fetched context in final state")
		}
	})
}

// testObservingState is a State wrapper that records content appends,
// for testing state decorators.
type testObservingState struct {
	upstream generators.State
	onAppend func(*generators.Content)
}

func (s testObservingState) AppendContent(content *generators.Content) (generators.State, error) {
	if s.onAppend != nil {
		s.onAppend(content)
	}
	newUpstream, err := s.upstream.AppendContent(content)
	if err != nil {
		return nil, err
	}
	return testObservingState{upstream: newUpstream, onAppend: s.onAppend}, nil
}

func (s testObservingState) Contents() iter.Seq[*generators.Content] {
	return s.upstream.Contents()
}

func (s testObservingState) Functions() iter.Seq[*generators.Function] {
	return s.upstream.Functions()
}

func (s testObservingState) SystemPrompt() string {
	return s.upstream.SystemPrompt()
}

func (s testObservingState) Flush() (generators.State, error) {
	newUpstream, err := s.upstream.Flush()
	if err != nil {
		return nil, err
	}
	return testObservingState{upstream: newUpstream, onAppend: s.onAppend}, nil
}

func (s testObservingState) Unwrap() generators.State {
	return s.upstream
}
