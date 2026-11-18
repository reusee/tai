package main

import (
	"strings"
	"testing"

	"github.com/reusee/tai/pipeline"
)

// TestGoSystemPromptSelectsDiscussionPrompt pins the Go session's prompt
// selection: a post-goal discussion runs on the discussion prompt, and
// every other session runs on the goal prompt. The goal prompt must not
// reach the discussion, because its done block and loop rules would
// apply to a conversation the user drives. See TheoryOfGoModuleDefault
// and pipeline.TheoryOfGoalDiscussion.
func TestGoSystemPromptSelectsDiscussionPrompt(t *testing.T) {
	goal := string(goSystemPrompt(
		pipeline.CodesComponents{}, "", nil, true, false,
	))
	if !strings.Contains(goal, "Goal-Directed Multi-Loop Execution") {
		t.Fatal("a goal session must run on the goal system prompt")
	}
	if strings.Contains(goal, "Post-Goal Discussion") {
		t.Fatal("a goal session must not carry the discussion prompt")
	}

	discuss := string(goSystemPrompt(
		pipeline.CodesComponents{}, "", nil, true, true,
	))
	if !strings.Contains(discuss, "Post-Goal Discussion") {
		t.Fatal("a discussion session must run on the discussion prompt")
	}
	if strings.Contains(discuss, "Goal-Directed Multi-Loop Execution") {
		t.Fatal("a discussion session must not carry the goal system prompt")
	}
}
