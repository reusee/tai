package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
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
// terminal error, if any. It drains the event iterator — events carry
// the run's notable occurrences (see TheoryOfLoopEvents) and only the
// final yield may carry the terminal error — and remembers the last
// non-nil error.
func runOnce(run Run, opts RunOptions) (Result, error) {
	var result Result
	var err error
	for _, e := range run(context.Background(), opts, &result) {
		if e != nil {
			err = e
		}
	}
	return result, err
}

// appendPhase creates a phase that appends text content and returns nil
// (end of phase chain).
func appendPhase(text string) generators.Phase {
	return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
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
func appendPhaseWithFinish(text string, finishReason string) generators.Phase {
	return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
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
// the attempt usage log record. See TheoryOfUsageLogging.
func appendPhaseWithUsage(text string, usage generators.Usage) generators.Phase {
	return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
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
func appendThenErrorPhase(text string, err error) generators.Phase {
	return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
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
func appendPhaseWithFlush(text string) generators.Phase {
	return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				// Emit an unclosed block (no closing delimiter) — a
				// parse error that must be fed back for self-correction.
				return appendPhaseWithFlush("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n")
			}
			// Second round: corrected output with a summary block.
			return appendPhaseWithFlush("<<龘靐 summary\nDone.\n龘靐\n")
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			// Persistently emit an unclosed block — a parse error every
			// generation. The correction loop must stop after
			// maxParseErrorCorrections.
			return appendPhaseWithFlush("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n")
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
		// Initial generation + maxParseErrorCorrections correction
		// generations.
		if callCount != 1+maxParseErrorCorrections {
			t.Fatalf("expected %d generations, got %d", 1+maxParseErrorCorrections, callCount)
		}
	})
}

func TestRunParseErrorCorrectionWithComponents(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				// A complete shell block followed by an unclosed change
				// block. The shell block is processed by the component;
				// the parse error feedback is prepended to the shell
				// output.
				return appendPhaseWithFlush("<<齉爩 shell\necho hi\n齉爩\n<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n")
			}
			return appendPhaseWithFlush("<<龘靐 summary\nDone.\n龘靐\n")
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
	// When components keep triggering generations, a model that
	// persistently emits malformed blocks must not restart the
	// parse-error correction cycle after the budget is exhausted. The
	// correction budget is cumulative per run: feedback is given only
	// for the first maxParseErrorCorrections generations with parse
	// errors, then stops until a clean generation resets the budget.
	// Uncorrected parse errors are surfaced in Result.ParseErrors. See
	// TheoryOfLoops.
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

		// Every generation emits a complete shell block (triggers a new
		// generation) plus an unclosed change block (parse error).
		phaseBuilder := func(g generators.Generator) generators.Phase {
			return appendPhaseWithFlush(
				"<<齉爩 shell\necho hi\n齉爩\n" +
					"<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n")
		}

		result, err := runOnce(run, RunOptions{
			Generator:      nil,
			InitialState:   generators.NewPrompts("", nil),
			Components:     comps,
			PhaseBuilder:   phaseBuilder,
			HTTPClient:     nets.HTTPClient{},
			MaxGenerations: 8,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Parse error feedback appears only in the first
		// maxParseErrorCorrections generations. Generations 4+ receive
		// only shell output.
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
		if feedbackCount != maxParseErrorCorrections {
			t.Fatalf("expected %d parse-error feedbacks (cumulative bound), got %d", maxParseErrorCorrections, feedbackCount)
		}

		// The uncorrected parse errors must be surfaced in the result.
		if len(result.ParseErrors) == 0 {
			t.Fatal("expected uncorrected parse errors in Result.ParseErrors")
		}
	})
}

// errorPhase creates a phase that returns an error.
func errorPhase(err error) generators.Phase {
	return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
		return nil, state, err
	}
}

func TestRunSingleRound(t *testing.T) {
	withRun(t, func(run Run) {
		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
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

func TestRunMultiGenerationTriggered(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("<<龘靐 shell\necho hello\n龘靐\n")
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
			t.Fatalf("expected 2 generations, got %d", callCount)
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

// TestRunAttemptNumbersContinueAcrossGenerations verifies the
// session-wide attempt counter: component-triggered generations
// continue the attempt sequence instead of restarting at 1, and each
// attempt-start also carries its position within the generation's
// retry budget. See TheoryOfLoopEvents.
func TestRunAttemptNumbersContinueAcrossGenerations(t *testing.T) {
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
			return appendPhase("<<龘靐 shell\necho hello\n龘靐\n")
		}
		var starts []Event
		var result Result
		for ev, err := range run(context.Background(), RunOptions{
			Generator:      nil,
			InitialState:   generators.NewPrompts("", nil),
			Components:     comps,
			PhaseBuilder:   phaseBuilder,
			HTTPClient:     nets.HTTPClient{},
			MaxGenerations: 2,
		}, &result) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev.Kind == EventAttemptStart {
				starts = append(starts, ev)
			}
		}
		if len(starts) != 2 {
			t.Fatalf("expected 2 attempt-start events, got %d", len(starts))
		}
		if starts[0].Attempt != 1 {
			t.Fatalf("first attempt number: got %d, want 1", starts[0].Attempt)
		}
		if starts[1].Attempt != 2 {
			t.Fatalf("second generation's attempt number must continue the session-wide sequence: got %d, want 2", starts[1].Attempt)
		}
		if starts[0].AttemptInGeneration != 1 || starts[1].AttemptInGeneration != 1 {
			t.Fatalf("each generation's first attempt is position 1 in the retry budget: got %d and %d",
				starts[0].AttemptInGeneration, starts[1].AttemptInGeneration)
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
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 shell\necho hi\n龘靐\n")
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
			PhaseBuilder: func(g generators.Generator) generators.Phase {
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				// No completion block — should trigger retry.
				return appendPhase("incomplete output without summary")
			}
			// Second call includes a summary block.
			return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
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

func TestRunRetryOnTriggeringBlockWithoutSummary(t *testing.T) {
	// A round that emits a component-triggering block (e.g., go-src)
	// but no summary block violates the every-response summary
	// requirement: the summary block is the completion signal, so the
	// round is retried with feedback naming the missing summary block,
	// and the triggering block from the failed attempt is discarded —
	// the model must re-emit it alongside the summary block. See
	// TheoryOfSummaryCompletionRetry and blocks.TheoryOfSummaryBlocks.
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			switch callCount {
			case 1:
				// A go-src block with no summary block.
				return appendPhase("<<建安 go-src\nfoo.Bar\n建安\n")
			case 2:
				// The retry re-emits the go-src block together with
				// the mandatory summary block.
				return appendPhase("<<建安 go-src\nfoo.Bar\n建安\n<<贞观 summary\nDone.\n贞观\n")
			default:
				return appendPhase("<<贞观 summary\nDone.\n贞观\n")
			}
		}

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

		result, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               comps,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			HTTPClient:               nets.HTTPClient{},
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 3 {
			t.Fatalf("expected 3 calls (retry once for missing summary, then component-triggered round), got %d", callCount)
		}

		foundFeedback := false
		foundResolved := false
		for c := range result.FinalState.Contents() {
			if c.Role != generators.RoleUser {
				continue
			}
			for _, p := range c.Parts {
				if text, ok := p.(generators.Text); ok {
					if strings.Contains(string(text), "WITHOUT the required summary block") {
						foundFeedback = true
					}
					if strings.Contains(string(text), "resolved source") {
						foundResolved = true
					}
				}
			}
		}
		if !foundFeedback {
			t.Fatal("expected missing-summary retry feedback in state")
		}
		if !foundResolved {
			t.Fatal("expected resolved source in state after retry")
		}
	})
}

func TestRunRetryExhaustedAppendsSummaryBlock(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		var successSummaries [][]string
		phaseBuilder := func(g generators.Generator) generators.Phase {
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
			OnAttemptSuccess: func(state generators.State, summaries []string) error {
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
			t.Fatalf("expected the synthesized summary in OnAttemptSuccess, got %v", successSummaries)
		}

		// The exhausted generation must have a synthesized summary block
		// appended to the state so the TUI's Events tab can display it.
		foundSummary := false
		for c := range result.FinalState.Contents() {
			for _, p := range c.Parts {
				if text, ok := p.(generators.Text); ok {
					if strings.Contains(string(text), "summary") &&
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				// Summary block present but finish reason is "length"
				// (max-token truncation). This should trigger retry
				// despite the summary block.
				return appendPhaseWithFinish("<<龘靐 summary\nDone.\n龘靐\n", "length")
			}
			// Second call: normal finish reason with summary.
			return appendPhaseWithFinish("<<龘靐 summary\nDone.\n龘靐\n", "stop")
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			return appendPhaseWithFinish("<<龘靐 summary\nDone.\n龘靐\n", "stop")
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
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

func TestRunOnAttemptStartCalled(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		onAttemptStart := func() {
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

		generation := 0
		_, err := runOnce(run, RunOptions{
			Generator:      nil,
			InitialState:   generators.NewPrompts("", nil),
			Components:     comps,
			OnAttemptStart: onAttemptStart,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				generation++
				if generation == 1 {
					return appendPhase("<<龘靐 shell\necho hi\n龘靐\n")
				}
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			},
			HTTPClient: nets.HTTPClient{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount < 2 {
			t.Fatalf("expected OnAttemptStart called at least 2 times, got %d", callCount)
		}
	})
}

func TestRunOnAttemptSuccessCalled(t *testing.T) {
	withRun(t, func(run Run) {
		var successStates []generators.State
		var successSummaries [][]string

		onAttemptSuccess := func(state generators.State, summaries []string) error {
			successStates = append(successStates, state)
			successSummaries = append(successSummaries, summaries)
			return nil
		}

		_, err := runOnce(run, RunOptions{
			Generator:        nil,
			InitialState:     generators.NewPrompts("", nil),
			Components:       nil,
			OnAttemptSuccess: onAttemptSuccess,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 summary\nRound 1 done.\n龘靐\n")
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(successStates) != 1 {
			t.Fatalf("expected 1 OnAttemptSuccess call, got %d", len(successStates))
		}
		if len(successSummaries) != 1 || len(successSummaries[0]) != 1 {
			t.Fatalf("expected 1 summary, got %v", successSummaries)
		}
		if !strings.Contains(successSummaries[0][0], "Round 1 done") {
			t.Fatalf("unexpected summary: %s", successSummaries[0][0])
		}
	})
}

func TestRunLogsAttemptUsage(t *testing.T) {
	// The Run loop must record the aggregated token usage of each
	// attempt to the logger, so token consumption is visible in log
	// output and in the TUI's Logs pane, not only in the
	// end-of-session statistics table. Streaming timings ride along as
	// one-decimal string keys so the fractional digit survives the text
	// handler. See TheoryOfUsageLogging and TheoryOfUsageTiming.
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
		usage.TimeToFirstToken = time.Second * 3 / 2 // logs as ttft_seconds=1.5
		usage.GenerateDuration = 2 * time.Second     // 60 generated tokens / 2s -> 30.0

		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhaseWithUsage("model output", usage)
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		for _, want := range []string{
			"msg=usage",
			"attempt=1",
			"prompt=100",
			"cached=20",
			"completion=50",
			"thoughts=10",
			"ttft_seconds=1.5",
			"tokens_per_second=30.0",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("expected %q in log output, got: %s", want, output)
			}
		}
	})
}

func TestRunHandoffUsageReachesAttemptUsageLog(t *testing.T) {
	// The handoff request's own token spend must reach the per-attempt
	// usage line: both attempts miss the summary, so the generation
	// ends via the exhausted-synthesis path whose injected usage lands
	// in attempt 2's final state — the logged usage (107/20/53/11) is
	// the main attempt's usage plus the handoff usage. Attempt 1's
	// truncation injection now also records its own usage entry; the
	// assertions below target attempt 2's entry. See
	// TheoryOfHandoffUsageAccounting and TheoryOfUsageLogging.
	var buf bytes.Buffer
	logger := logs.Logger{slog.New(slog.NewTextHandler(&buf, nil))}
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() logs.Logger { return logger },
	).Call(func(run Run) {
		mainUsage := generators.Usage{}
		mainUsage.Prompt.TokenCount = 100
		mainUsage.Prompt.TokenCountCached = 20
		mainUsage.Candidates.TokenCount = 50
		mainUsage.Thoughts.TokenCount = 10
		handoffUsage := generators.Usage{}
		handoffUsage.Prompt.TokenCount = 7
		handoffUsage.Candidates.TokenCount = 3
		handoffUsage.Thoughts.TokenCount = 1

		callCount := 0
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				callCount++
				return appendPhaseWithUsage(
					fmt.Sprintf("incomplete attempt %d output", callCount),
					mainUsage,
				)
			},
			RetryOnMissingCompletion: true,
			MaxRetries:               1,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "handoff summary", Prompt: "retry prompt", Usage: handoffUsage}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		for _, want := range []string{
			"msg=usage",
			"attempt=2",
			"prompt=107",
			"cached=20",
			"completion=53",
			"thoughts=11",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("expected %q in log output, got: %s", want, output)
			}
		}
	})
}

func TestRunHandoffUsageRetryWindowIsPerAttempt(t *testing.T) {
	// On error retries the partial output is retained in the state, so
	// a prior attempt's usage part stays visible in later attempts'
	// states. The handoff usage injection must window from the failed
	// attempt's own base: an attempt that errors before emitting a usage
	// part must not have the prior attempt's usage re-attributed to it.
	// See TheoryOfHandoffUsageAccounting.
	withRun(t, func(run Run) {
		usage1 := generators.Usage{}
		usage1.Prompt.TokenCount = 100
		usage1.Prompt.TokenCountCached = 20
		usage1.Candidates.TokenCount = 50
		usage1.Thoughts.TokenCount = 10
		handoffUsage := generators.Usage{}
		handoffUsage.Prompt.TokenCount = 7
		handoffUsage.Candidates.TokenCount = 3
		handoffUsage.Thoughts.TokenCount = 1

		texts := []string{
			"first partial output",
			"second partial output",
			"third partial output",
		}
		callCount := 0
		var truncatedStates []generators.State
		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
					callCount++
					newState, err := state.AppendContent(&generators.Content{
						Role:  generators.RoleAssistant,
						Parts: []generators.Part{generators.Text(texts[callCount-1])},
					})
					if err != nil {
						return nil, state, err
					}
					if callCount == 1 {
						// Only the first attempt reports usage; the later
						// attempts error before emitting a usage part.
						newState, err = newState.AppendContent(&generators.Content{
							Role:  generators.RoleLog,
							Parts: []generators.Part{usage1},
						})
						if err != nil {
							return nil, state, err
						}
					}
					return nil, newState, errors.New("boom")
				}
			},
			RetryOnError: true,
			MaxRetries:   2,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "handoff summary", Prompt: "retry prompt", Usage: handoffUsage}, nil
			},
			OnAttemptTruncated: func(truncatedState generators.State, retryBaseState generators.State, summary string) error {
				truncatedStates = append(truncatedStates, truncatedState)
				return nil
			},
		})
		if err == nil {
			t.Fatal("expected a terminal error after the retry budget was exhausted")
		}
		if len(truncatedStates) != 2 {
			t.Fatalf("expected 2 truncated records, got %d", len(truncatedStates))
		}
		first := extractLastUsage(truncatedStates[0], 0)
		if first.Prompt.TokenCount != 107 || first.Prompt.TokenCountCached != 20 ||
			first.Candidates.TokenCount != 53 || first.Thoughts.TokenCount != 11 {
			t.Fatalf("expected first injection (107, 20, 53, 11), got (%d, %d, %d, %d)",
				first.Prompt.TokenCount, first.Prompt.TokenCountCached,
				first.Candidates.TokenCount, first.Thoughts.TokenCount)
		}
		second := extractLastUsage(truncatedStates[1], 0)
		if second.Prompt.TokenCount != 7 || second.Prompt.TokenCountCached != 0 ||
			second.Candidates.TokenCount != 3 || second.Thoughts.TokenCount != 1 {
			t.Fatalf("expected second injection (7, 0, 3, 1) — the attempt's own window only — got (%d, %d, %d, %d)",
				second.Prompt.TokenCount, second.Prompt.TokenCountCached,
				second.Candidates.TokenCount, second.Thoughts.TokenCount)
		}
	})
}

func TestRunLogsRoundUsageMultipleUsageParts(t *testing.T) {
	// If a generator emits multiple Usage parts during streaming (e.g., Gemini),
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
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
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

func TestRunOnAttemptSuccessError(t *testing.T) {
	withRun(t, func(run Run) {
		expectedErr := errors.New("flush failed")
		onAttemptSuccess := func(state generators.State, summaries []string) error {
			return expectedErr
		}

		_, err := runOnce(run, RunOptions{
			Generator:        nil,
			InitialState:     generators.NewPrompts("", nil),
			Components:       nil,
			OnAttemptSuccess: onAttemptSuccess,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("hello")
			},
		})
		if err == nil {
			t.Fatal("expected error from OnAttemptSuccess")
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
			PhaseBuilder: func(g generators.Generator) generators.Phase {
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

func TestRunMaxGenerations(t *testing.T) {
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
			Generator:      nil,
			InitialState:   generators.NewPrompts("", nil),
			Components:     comps,
			MaxGenerations: 3,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				callCount++
				return appendPhase("<<龘靐 shell\necho hi\n龘靐\n")
			},
			HTTPClient: nets.HTTPClient{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 3 {
			t.Fatalf("expected 3 generations (MaxGenerations), got %d", callCount)
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
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendThenErrorPhase("partial model output", errors.New("something went wrong"))
			}
			return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
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
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendThenErrorPhase(
					"partial model output",
					&changes.ApplyError{Err: errors.New("apply change block MODIFY Foo: target not found")},
				)
			}
			return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			return appendThenErrorPhase("some output", errors.New("always fails"))
		}

		_, err := runOnce(run, RunOptions{
			Generator:      nil,
			InitialState:   generators.NewPrompts("", nil),
			Components:     nil,
			PhaseBuilder:   phaseBuilder,
			OnAttemptStart: func() {},
			RetryOnError:   true,
			MaxRetries:     2,
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
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendPhase("incomplete output without summary")
				}
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
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
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendThenErrorPhase("partial output", errors.New("some error"))
				}
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
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
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendPhase("incomplete output without summary")
				}
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
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
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				if callCount == 1 {
					return appendThenErrorPhase("partial output", errors.New("some error"))
				}
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
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

func TestRunOnAttemptTruncatedCalled(t *testing.T) {
	withRun(t, func(run Run) {
		var truncatedSummaries []string
		onAttemptTruncated := func(truncatedState generators.State, retryBaseState generators.State, summary string) error {
			truncatedSummaries = append(truncatedSummaries, summary)
			return nil
		}

		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("incomplete output without summary")
			}
			return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
		}

		_, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			OnAttemptTruncated:       onAttemptTruncated,
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
			t.Fatalf("expected 1 OnAttemptTruncated call, got %d", len(truncatedSummaries))
		}
		if truncatedSummaries[0] != "truncated summary" {
			t.Fatalf("expected 'truncated summary', got %q", truncatedSummaries[0])
		}
	})
}

func TestRunRetryPromptIsIncludedDirectly(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("incomplete output without summary")
			}
			return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
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
	if !strings.Contains(msg, "no completed work") {
		t.Fatalf("expected the atomicity note in the retry prompt, got: %s", msg)
	}
	if !strings.Contains(msg, "no next step to carry forward") {
		t.Fatalf("expected the no-next-step note in the retry prompt, got: %s", msg)
	}
	if !strings.Contains(msg, "partition the work") {
		t.Fatalf("expected partitioning instruction in the retry prompt, got: %s", msg)
	}
	if !strings.Contains(msg, "continue block") {
		t.Fatalf("expected continue block guidance in the retry prompt, got: %s", msg)
	}
	if strings.Contains(msg, "Re-read the current filesystem state") {
		t.Fatalf("the retry prompt must not instruct re-reading the filesystem, got: %s", msg)
	}
	if strings.Contains(msg, "summary") && strings.Contains(msg, "<<") {
		t.Fatalf("expected no summary block in the retry prompt, got: %s", msg)
	}
	if strings.Contains(msg, "continue") && strings.Contains(msg, "<<") {
		t.Fatalf("expected no continue block in the retry prompt, got: %s", msg)
	}
}

func TestRunOnErrorNoRetryWhenDisabled(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
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
		phaseBuilder := func(g generators.Generator) generators.Phase {
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

		phaseBuilder := func(g generators.Generator) generators.Phase {
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

		onIdle := IdleHandler(func(ctx context.Context, state generators.State) (generators.State, bool, error) {
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

		phaseBuilder := func(g generators.Generator) generators.Phase {
			genCount++
			return appendPhase("<<龘靐 shell\necho hi\n龘靐\n")
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

		onIdle := IdleHandler(func(ctx context.Context, state generators.State) (generators.State, bool, error) {
			idleCount++
			return state, false, nil
		})

		_, err := runOnce(run, RunOptions{
			Generator:      nil,
			InitialState:   generators.NewPrompts("", nil),
			Components:     comps,
			PhaseBuilder:   phaseBuilder,
			OnIdle:         onIdle,
			MaxGenerations: 3,
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
		onIdle := IdleHandler(func(ctx context.Context, state generators.State) (generators.State, bool, error) {
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
			PhaseBuilder: func(g generators.Generator) generators.Phase {
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

		phaseBuilder := func(g generators.Generator) generators.Phase {
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
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("<<龘靐 done\ngoal achieved\n龘靐\n<<齉爩 other\ntrigger\n齉爩\n")
			}
			return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
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
			PhaseBuilder: func(g generators.Generator) generators.Phase {
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
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				// The state-modifier block is emitted together with
				// the mandatory summary block: no block kind completes
				// a round on its own, so a triggering block without a
				// summary would be retried rather than processed.
				// See TheoryOfSummaryCompletionRetry.
				return appendPhase("<<建安 state-modifier\nrequest\n建安\n<<贞观 summary\nRound 1 done.\n贞观\n")
			}
			return appendPhase("<<贞观 summary\nDone.\n贞观\n")
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
			t.Fatalf("expected 2 rounds (state modification triggers next), got %d", callCount)
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
