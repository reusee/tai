package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/pipeline"
)

func TestGoalCommandRegistered(t *testing.T) {
	dscope.New(
		new(Module),
	).Call(func(
		cmd Command,
	) {
		keys := cmd.Keys()
		if _, ok := keys["goal"]; !ok {
			t.Fatal("goal command not registered in Keys()")
		}

		newValue, remainArgs, err := cmd.Handle("goal", []string{"chat", "test goal"})
		if err != nil {
			t.Fatalf("Handle goal failed: %v", err)
		}
		goalCmd, ok := newValue.(*Command)
		if !ok {
			t.Fatal("Handle goal did not return a *Command")
		}
		if goalCmd.Main == nil {
			t.Fatal("GoalCommand has no Main")
		}
		if len(remainArgs) != 2 || remainArgs[0] != "chat" || remainArgs[1] != "test goal" {
			t.Fatalf("expected [chat test goal], got %v", remainArgs)
		}
	})
}

func TestGoalSystemPromptContent(t *testing.T) {
	if !strings.Contains(GoalSystemPrompt, "Goal-Directed Multi-Loop Execution") {
		t.Fatal("GoalSystemPrompt must contain goal-directed multi-loop execution header")
	}
	if !strings.Contains(GoalSystemPrompt, "done block") {
		t.Fatal("GoalSystemPrompt must reference done block for goal completion")
	}
	if strings.Contains(GoalSystemPrompt, ".GOAL_COMPLETE") {
		t.Fatal("GoalSystemPrompt must not reference .GOAL_COMPLETE marker file")
	}
	if !strings.Contains(GoalSystemPrompt, `(kind "done")`) {
		t.Fatal("GoalSystemPrompt must describe the done block kind")
	}
}

func TestGoalSystemPromptNoLiteralDelimiter(t *testing.T) {
	if strings.Contains(GoalSystemPrompt, "<<DELIMITER") {
		t.Fatal("GoalSystemPrompt must not display the literal template marker '<<DELIMITER'")
	}
}

func TestGoalTheoryStatesNoProcessLevelCaches(t *testing.T) {
	// The generation pipeline must not hold process-level caches: all
	// caches live inside scope provider functions, so dscope.Reset
	// recomputes them on every goal loop. See TheoryOfGoalCommand.
	if !strings.Contains(TheoryOfGoalCommand, "no process-level caches") {
		t.Fatal("TheoryOfGoalCommand must state that the generation pipeline holds no process-level caches")
	}
	if !strings.Contains(TheoryOfGoalCommand, "scope provider functions") {
		t.Fatal("TheoryOfGoalCommand must state that all generation caches live inside scope provider functions")
	}
}

func TestGoalCommandStopsAfterDoneBlock(t *testing.T) {
	// A done block is a declaration, not a verdict: the goal command must
	// run one more loop to verify the declaration against the current
	// filesystem state, and only a second consecutive done block confirms
	// the goal. The loop body runs inside a closure passed to scope.Call,
	// so a return there only exits the closure; if the loop continued
	// after confirmation, the "Goal Not Achieved" message would appear,
	// failing this test. See TheoryOfGoalCommand.
	calls := 0
	fakeScope := dscope.New(
		func() pipeline.GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (pipeline.Result, []pipeline.AttemptStat, error) {
				calls++
				return pipeline.Result{
					RemainingBlocks: []blocks.Block{{Kind: "done"}},
				}, nil, nil
			}
		},
	)
	reset := dscope.Reset(func() dscope.Scope { return fakeScope })

	// Redirect stdout to capture the command's output.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	mainFn := GoalCommand.Main.(func(Output, dscope.Reset, pipeline.RunReview))
	mainFn(Output(os.Stdout), reset, pipeline.RunReview(func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
		return nil
	}))

	w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()

	// The first loop declares completion; the second loop verifies the
	// declaration against the current filesystem state and confirms it.
	if calls != 2 {
		t.Fatalf("expected 2 calls (declaration + verification), got %d", calls)
	}
	if !strings.Contains(string(output), "Goal Achieved") {
		t.Fatal("expected goal achieved message when done block confirmed by verification loop")
	}
	if strings.Contains(string(output), "Goal Not Achieved") {
		t.Fatal("goal not achieved message must not appear when done block confirmed")
	}
}

func TestGoalCommandReportsParseErrors(t *testing.T) {
	// Uncorrected malformed blocks must be reported per goal loop so
	// silent change loss is surfaced in unattended operation.
	// See TheoryOfGoalCommand.
	fakeScope := dscope.New(
		func() pipeline.GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (pipeline.Result, []pipeline.AttemptStat, error) {
				return pipeline.Result{
					ParseErrors: []*blocks.BlockParseError{
						{BlockKind: "change", Boundary: "龘靐"},
					},
				}, nil, nil
			}
		},
	)
	reset := dscope.Reset(func() dscope.Scope { return fakeScope })

	// Redirect stdout and stderr to capture the command's output.
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wOut
	os.Stderr = wErr

	mainFn := GoalCommand.Main.(func(Output, dscope.Reset, pipeline.RunReview))
	mainFn(Output(os.Stdout), reset, pipeline.RunReview(func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
		return nil
	}))

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	stdout, err := io.ReadAll(rOut)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(rErr)
	if err != nil {
		t.Fatal(err)
	}
	rOut.Close()
	rErr.Close()

	if !strings.Contains(string(stderr), "malformed block(s) could not be corrected") {
		t.Fatalf("expected parse error report in stderr, got: %s", string(stderr))
	}
	if !strings.Contains(string(stderr), "change") {
		t.Fatalf("expected block kind in parse error report, got: %s", string(stderr))
	}
	_ = stdout
}

func TestGoalCommandUnattendedErrorRecovery(t *testing.T) {
	t.Run("StopsAfterRepeatedErrors", func(t *testing.T) {
		// When the same error occurs maxConsecutiveGoalErrors times in a
		// row, the goal command must stop early instead of burning the
		// remaining iterations on a persistent failure in unattended
		// operation. See TheoryOfGoalCommand.
		calls := 0
		fakeScope := dscope.New(
			func() pipeline.GenerateWithResultWithStats {
				return func(ctx context.Context, output io.Writer) (pipeline.Result, []pipeline.AttemptStat, error) {
					calls++
					return pipeline.Result{}, nil, errors.New("persistent failure")
				}
			},
		)
		reset := dscope.Reset(func() dscope.Scope { return fakeScope })

		oldStdout := os.Stdout
		oldStderr := os.Stderr
		rOut, wOut, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		rErr, wErr, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = wOut
		os.Stderr = wErr

		mainFn := GoalCommand.Main.(func(Output, dscope.Reset, pipeline.RunReview))
		mainFn(Output(os.Stdout), reset, pipeline.RunReview(func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
			return nil
		}))

		wOut.Close()
		wErr.Close()
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		stdout, err := io.ReadAll(rOut)
		if err != nil {
			t.Fatal(err)
		}
		stderr, err := io.ReadAll(rErr)
		if err != nil {
			t.Fatal(err)
		}
		rOut.Close()
		rErr.Close()

		if calls != maxConsecutiveGoalErrors {
			t.Fatalf("expected %d calls before stop, got %d", maxConsecutiveGoalErrors, calls)
		}
		if !strings.Contains(string(stderr), "same error occurred") {
			t.Fatalf("expected repeated error diagnostic in stderr, got: %s", string(stderr))
		}
		if strings.Contains(string(stdout), "Goal Not Achieved") {
			t.Fatalf("expected no 'Goal Not Achieved' message when stopping early, got: %s", string(stdout))
		}
	})

	t.Run("CarriesFeedbackToNextLoop", func(t *testing.T) {
		// The goal command must carry the previous loop's failure into
		// the next loop's system prompt so the model can correct its
		// approach in unattended operation. A done block emitted after a
		// failure is a declaration, so a third loop is needed to verify it
		// before the goal is confirmed. See TheoryOfGoalCommand.
		calls := 0
		var seenPrompts []string
		fakeScope := dscope.New(
			func() GoalFeedback { return "" },
			func(
				systemPrompt pipeline.SystemPrompt,
			) pipeline.GenerateWithResultWithStats {
				return func(ctx context.Context, output io.Writer) (pipeline.Result, []pipeline.AttemptStat, error) {
					calls++
					seenPrompts = append(seenPrompts, string(systemPrompt))
					if calls == 1 {
						return pipeline.Result{}, nil, errors.New("first loop failed")
					}
					return pipeline.Result{
						RemainingBlocks: []blocks.Block{{Kind: "done"}},
					}, nil, nil
				}
			},
			func(
				feedback GoalFeedback,
			) pipeline.SystemPrompt {
				return pipeline.SystemPrompt("BASE_PROMPT" + string(feedback))
			},
		)
		reset := dscope.Reset(func() dscope.Scope { return fakeScope })

		oldStdout := os.Stdout
		oldStderr := os.Stderr
		rOut, wOut, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		rErr, wErr, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = wOut
		os.Stderr = wErr

		mainFn := GoalCommand.Main.(func(Output, dscope.Reset, pipeline.RunReview))
		mainFn(Output(os.Stdout), reset, pipeline.RunReview(func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
			return nil
		}))

		wOut.Close()
		wErr.Close()
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		stdout, err := io.ReadAll(rOut)
		if err != nil {
			t.Fatal(err)
		}
		stderr, err := io.ReadAll(rErr)
		if err != nil {
			t.Fatal(err)
		}
		rOut.Close()
		rErr.Close()

		if calls != 3 {
			t.Fatalf("expected 3 calls (failure, done declaration, verification), got %d", calls)
		}
		if len(seenPrompts) != 3 {
			t.Fatalf("expected 3 prompts, got %d", len(seenPrompts))
		}
		if strings.Contains(seenPrompts[0], "first loop failed") {
			t.Fatalf("first prompt must not contain feedback, got: %s", seenPrompts[0])
		}
		if !strings.Contains(seenPrompts[1], "first loop failed") {
			t.Fatalf("expected feedback in second prompt, got: %s", seenPrompts[1])
		}
		if !strings.Contains(seenPrompts[2], goalDoneVerificationPrompt) {
			t.Fatalf("expected verification feedback in third prompt, got: %s", seenPrompts[2])
		}
		_ = stdout
		_ = stderr
	})
}

func TestGoalCommandAggregatesStatistics(t *testing.T) {
	// The goal command must retain each loop's attempt statistics and
	// print them once more, aggregated, after the goal completes — in
	// addition to the per-loop print at each loop's end — so the user
	// can review the entire process in a single table. The Loop column
	// identifies which goal loop produced each attempt. See
	// pipeline.TheoryOfAttemptStatistics.
	//
	// With done-block verification, the command runs two loops (declaration
	// and verification), so the aggregated totals cover both loops.
	// See TheoryOfGoalCommand.
	fakeScope := dscope.New(
		func() pipeline.GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (pipeline.Result, []pipeline.AttemptStat, error) {
				return pipeline.Result{
					RemainingBlocks: []blocks.Block{{Kind: "done"}},
				}, []pipeline.AttemptStat{
					{Attempt: 1, PromptTokens: 111, CompletionTokens: 51, Duration: time.Second, Summary: "first round"},
					{Attempt: 2, PromptTokens: 222, CompletionTokens: 82, Duration: time.Second, Summary: "second round"},
				}, nil
			}
		},
	)
	reset := dscope.Reset(func() dscope.Scope { return fakeScope })

	// Redirect stdout to capture the command's output.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	mainFn := GoalCommand.Main.(func(Output, dscope.Reset, pipeline.RunReview))
	mainFn(Output(os.Stdout), reset, pipeline.RunReview(func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
		return nil
	}))

	w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()

	outputStr := string(output)
	// The aggregated statistics must be printed after the goal completes,
	// with the goal-specific title, the Loop column, and totals across all
	// loops (111 + 222 = 333 prompt tokens per loop, 333 x 2 loops = 666).
	if !strings.Contains(outputStr, "Goal Loop Statistics") {
		t.Fatal("expected aggregated goal loop statistics after goal completes")
	}
	if !strings.Contains(outputStr, "Loop 1 Attempt 1: first round") {
		t.Fatal("expected loop-aware summary line for attempt 1 in aggregated statistics")
	}
	if !strings.Contains(outputStr, "Loop 1 Attempt 2: second round") {
		t.Fatal("expected loop-aware summary line for attempt 2 in aggregated statistics")
	}
	if !strings.Contains(outputStr, "Loop 2 Attempt 1: first round") {
		t.Fatal("expected loop-aware summary line for attempt 1 of the verification loop in aggregated statistics")
	}
	if !strings.Contains(outputStr, "Loop 2 Attempt 2: second round") {
		t.Fatal("expected loop-aware summary line for attempt 2 of the verification loop in aggregated statistics")
	}
	if !strings.Contains(outputStr, "666") {
		t.Fatal("expected aggregated totals across all loops")
	}
}

func TestGoalCommandVerifiesDoneBlockInFreshLoop(t *testing.T) {
	// A done block is a declaration, not a verdict: the filesystem may
	// have changed while the declaring loop ran (e.g., todo.md may have
	// gained new tasks), so the goal must be re-assessed in a fresh loop
	// that reads the current filesystem state. When the verification loop
	// finds remaining work and emits no done block, the declaration is
	// overturned and the command continues running until the loop budget
	// is exhausted. See TheoryOfGoalCommand.
	calls := 0
	fakeScope := dscope.New(
		func() pipeline.GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (pipeline.Result, []pipeline.AttemptStat, error) {
				calls++
				if calls == 1 {
					// First loop: declares the goal achieved.
					return pipeline.Result{
						RemainingBlocks: []blocks.Block{{Kind: "done"}},
					}, nil, nil
				}
				// Verification loop and all subsequent loops: re-read the
				// filesystem, find remaining work, emit no done block.
				return pipeline.Result{}, nil, nil
			}
		},
	)
	reset := dscope.Reset(func() dscope.Scope { return fakeScope })

	// Redirect stdout to capture the command's output.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	mainFn := GoalCommand.Main.(func(Output, dscope.Reset, pipeline.RunReview))
	mainFn(Output(os.Stdout), reset, pipeline.RunReview(func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
		return nil
	}))

	w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()

	// The command must continue after the verification loop overturns the
	// declaration, running until the loop budget is exhausted.
	if calls != maxGoalIterations {
		t.Fatalf("expected %d calls, got %d", maxGoalIterations, calls)
	}
	if strings.Contains(string(output), "Goal Achieved") {
		t.Fatal("goal achieved must not appear when the verification loop overturns the declaration")
	}
	if !strings.Contains(string(output), "Goal Not Achieved") {
		t.Fatal("expected goal not achieved message when the verification loop overturns the declaration")
	}
}
