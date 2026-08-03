package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

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
		t.Fatal("GoalSystemPrompt must mandate the two-uncommon-Chinese-characters delimiter policy")
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
		func() codes.GenerateWithResult {
			return func(ctx context.Context, output io.Writer) (loops.Result, error) {
				return loops.Result{
					RemainingBlocks: []blocks.Block{{Kind: "done"}},
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

	if !strings.Contains(string(output), "Goal Achieved") {
		t.Fatal("expected goal achieved message when done block present")
	}
	if strings.Contains(string(output), "Goal Not Achieved") {
		t.Fatal("goal not achieved message must not appear when done block found; the loop must stop after the first loop")
	}
}
