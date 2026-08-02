package main

import (
	"strings"
	"testing"

	"github.com/reusee/dscope"
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
	if !strings.Contains(GoalSystemPrompt, ".GOAL_COMPLETE") {
		t.Fatal("GoalSystemPrompt must reference .GOAL_COMPLETE marker file")
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
