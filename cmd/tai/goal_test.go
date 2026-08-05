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
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/loops"
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
	if !strings.Contains(GoalSystemPrompt, "uncommon Chinese characters") {
		t.Fatal("GoalSystemPrompt must mandate the three-uncommon-Chinese-characters delimiter policy")
	}
}

func TestGoalSystemPromptNoLiteralDelimiter(t *testing.T) {
	if strings.Contains(GoalSystemPrompt, "<<DELIMITER") {
		t.Fatal("GoalSystemPrompt must not display the literal template marker '<<DELIMITER'")
	}
}

func TestGoalTheoryStatesNoProcessLevelCaches(t *testing.T) {
	// The gocodes pipeline must not hold process-level caches: all caches
	// live inside scope provider functions, so dscope.Reset recomputes them
	// on every goal loop. See TheoryOfGoalCommand.
	if !strings.Contains(TheoryOfGoalCommand, "no process-level caches") {
		t.Fatal("TheoryOfGoalCommand must state that the gocodes pipeline holds no process-level caches")
	}
	if !strings.Contains(TheoryOfGoalCommand, "scope provider functions") {
		t.Fatal("TheoryOfGoalCommand must state that all gocodes caches live inside scope provider functions")
	}
}

func TestGoalCommandStopsAfterDoneBlock(t *testing.T) {
	// The loop body runs inside a closure passed to scope.Call, so a
	// return there only exits the closure. If the loop continues after
	// a done block, the "Goal Not Achieved" message appears, failing
	// this test. See TheoryOfGoalCommand.
	fakeScope := dscope.New(
		func() codes.GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (loops.Result, []codes.RoundStat, error) {
				return loops.Result{
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

	mainFn := GoalCommand.Main.(func(dscope.Reset))
	mainFn(reset)

	w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()

	if !strings.Contains(string(output), "Goal Achieved") {
		t.Fatal("expected goal achieved message when done block present")
	}
	if strings.Contains(string(output), "Goal Not Achieved") {
		t.Fatal("goal not achieved message must not appear when done block found; the loop must stop after the first loop")
	}
}

func TestGoalCommandReportsParseErrors(t *testing.T) {
	// Uncorrected malformed blocks must be reported per goal loop so
	// silent change loss is surfaced in unattended operation.
	// See TheoryOfGoalCommand.
	fakeScope := dscope.New(
		func() codes.GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (loops.Result, []codes.RoundStat, error) {
				return loops.Result{
					ParseErrors: []*blocks.BlockParseError{
						{BlockKind: "change", Boundary: "徕珑龘"},
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

	mainFn := GoalCommand.Main.(func(dscope.Reset))
	mainFn(reset)

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
			func() codes.GenerateWithResultWithStats {
				return func(ctx context.Context, output io.Writer) (loops.Result, []codes.RoundStat, error) {
					calls++
					return loops.Result{}, nil, errors.New("persistent failure")
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

		mainFn := GoalCommand.Main.(func(dscope.Reset))
		mainFn(reset)

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
		// approach in unattended operation. See TheoryOfGoalCommand.
		calls := 0
		var seenPrompts []string
		fakeScope := dscope.New(
			func() GoalFeedback { return "" },
			func(
				systemPrompt codes.SystemPrompt,
			) codes.GenerateWithResultWithStats {
				return func(ctx context.Context, output io.Writer) (loops.Result, []codes.RoundStat, error) {
					calls++
					seenPrompts = append(seenPrompts, string(systemPrompt))
					if calls == 1 {
						return loops.Result{}, nil, errors.New("first loop failed")
					}
					return loops.Result{
						RemainingBlocks: []blocks.Block{{Kind: "done"}},
					}, nil, nil
				}
			},
			func(
				feedback GoalFeedback,
			) codes.SystemPrompt {
				return codes.SystemPrompt("BASE_PROMPT" + string(feedback))
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

		mainFn := GoalCommand.Main.(func(dscope.Reset))
		mainFn(reset)

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

		if calls != 2 {
			t.Fatalf("expected 2 calls, got %d", calls)
		}
		if len(seenPrompts) != 2 {
			t.Fatalf("expected 2 prompts, got %d", len(seenPrompts))
		}
		if strings.Contains(seenPrompts[0], "first loop failed") {
			t.Fatalf("first prompt must not contain feedback, got: %s", seenPrompts[0])
		}
		if !strings.Contains(seenPrompts[1], "first loop failed") {
			t.Fatalf("expected feedback in second prompt, got: %s", seenPrompts[1])
		}
		_ = stdout
		_ = stderr
	})
}

func TestGoalCommandAggregatesStatistics(t *testing.T) {
	// The goal command must retain each loop's round statistics and print
	// them once more, aggregated, after the goal completes — in addition to
	// the per-loop print at each loop's end — so the user can review the
	// entire process in a single table. The Loop column identifies which
	// goal loop produced each round. See codes.TheoryOfRoundStatistics.
	fakeScope := dscope.New(
		func() codes.GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (loops.Result, []codes.RoundStat, error) {
				return loops.Result{
					RemainingBlocks: []blocks.Block{{Kind: "done"}},
				}, []codes.RoundStat{
					{Round: 1, PromptTokens: 111, CompletionTokens: 51, Duration: time.Second, Summary: "first round"},
					{Round: 2, PromptTokens: 222, CompletionTokens: 82, Duration: time.Second, Summary: "second round"},
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

	mainFn := GoalCommand.Main.(func(dscope.Reset))
	mainFn(reset)

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
	// loops (111 + 222 = 333 prompt tokens).
	if !strings.Contains(outputStr, "Goal Loop Statistics") {
		t.Fatal("expected aggregated goal loop statistics after goal completes")
	}
	if !strings.Contains(outputStr, "Loop 1 Round 1: first round") {
		t.Fatal("expected loop-aware summary line for round 1 in aggregated statistics")
	}
	if !strings.Contains(outputStr, "Loop 1 Round 2: second round") {
		t.Fatal("expected loop-aware summary line for round 2 in aggregated statistics")
	}
	if !strings.Contains(outputStr, "333") {
		t.Fatal("expected aggregated totals across all loops")
	}
}
