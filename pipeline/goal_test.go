package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/reusee/prompts"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
)

// doneResult builds a Result whose only remaining block is a done block:
// the done kind is not consumed by any component, so it lands in
// RemainingBlocks where the goal runner detects it. See TheoryOfGoalMode.
func doneResult() Result {
	return Result{
		RemainingBlocks: []blocks.Block{{Kind: "done"}},
	}
}

// noopReview is a review stub for goal tests.
func noopReview(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
	return nil
}

func TestRunGoalConfirmsDoneAfterVerificationLoop(t *testing.T) {
	output := &bytes.Buffer{}
	calls := 0
	var feedbacks []GoalFeedback
	result := RunGoal(context.Background(), GoalOptions{
		Output: output,
		Generate: func(ctx context.Context, feedback GoalFeedback, _ GoalLoopSummaries) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !result.Achieved {
		t.Fatal("goal must be achieved after two consecutive done loops")
	}
	if result.LoopsRun != 2 {
		t.Fatalf("ran %d loops, want 2", result.LoopsRun)
	}
	if len(feedbacks) != 2 {
		t.Fatalf("got %d generate calls, want 2", len(feedbacks))
	}
	if feedbacks[0] != "" {
		t.Fatalf("first loop feedback = %q, want empty", feedbacks[0])
	}
	if !strings.Contains(string(feedbacks[1]), "done block") {
		t.Fatalf("verification loop feedback must carry the done verification prompt, got %q", feedbacks[1])
	}
	if !strings.Contains(string(feedbacks[1]), "Verification is the primary work") {
		t.Fatalf("verification loop feedback must state that verification is the primary work, got %q", feedbacks[1])
	}
	if !strings.Contains(output.String(), "Goal Achieved") {
		t.Fatal("output must report the achieved goal")
	}
}

func TestRunGoalCarriesErrorFeedback(t *testing.T) {
	calls := 0
	var feedbacks []GoalFeedback
	RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, feedback GoalFeedback, _ GoalLoopSummaries) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			if calls == 1 {
				return Result{}, nil, errors.New("api down")
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !strings.Contains(string(feedbacks[1]), "previous goal loop failed") {
		t.Fatalf("feedback after failure must carry the failure note, got %q", feedbacks[1])
	}
}

// TestRunGoalCarriesPreviousLoopSummaries verifies that every loop after
// the first receives the accumulated summaries of all previous loops,
// including summaries from a loop that ended in an error.
func TestRunGoalCarriesPreviousLoopSummaries(t *testing.T) {
	calls := 0
	var summaries []GoalLoopSummaries
	RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, feedback GoalFeedback, s GoalLoopSummaries) (Result, []AttemptStat, error) {
			calls++
			summaries = append(summaries, s)
			if calls == 1 {
				return Result{}, []AttemptStat{
					{Attempt: 1, Summary: "explored the parser"},
					{Attempt: 2},
					{Attempt: 3, Summary: "fixed the boundary bug"},
				}, errors.New("api down")
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if calls != 3 {
		t.Fatalf("ran %d loops, want 3", calls)
	}
	if len(summaries[0]) != 0 {
		t.Fatalf("first loop must see no summaries, got %v", summaries[0])
	}
	if len(summaries[1]) != 2 {
		t.Fatalf("second loop must see 2 summaries, got %d", len(summaries[1]))
	}
	if summaries[1][0] != (GoalLoopSummary{Loop: 1, Text: "explored the parser"}) ||
		summaries[1][1] != (GoalLoopSummary{Loop: 1, Text: "fixed the boundary bug"}) {
		t.Fatalf("summaries = %+v, want the two non-empty loop-1 attempt summaries", summaries[1])
	}
	if len(summaries[2]) != 2 {
		t.Fatalf("verification loop must still see 2 summaries, got %d", len(summaries[2]))
	}
}

func TestRunGoalStopsAfterConsecutiveSameErrors(t *testing.T) {
	calls := 0
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, feedback GoalFeedback, _ GoalLoopSummaries) (Result, []AttemptStat, error) {
			calls++
			return Result{}, nil, errors.New("persistent failure")
		},
		Review: noopReview,
	})
	if calls != maxConsecutiveGoalErrors {
		t.Fatalf("ran %d loops, want %d", calls, maxConsecutiveGoalErrors)
	}
	if result.LoopsRun != maxConsecutiveGoalErrors {
		t.Fatalf("LoopsRun = %d, want %d", result.LoopsRun, maxConsecutiveGoalErrors)
	}
	if result.Achieved {
		t.Fatal("goal must not be achieved")
	}
}

func TestRunGoalParseErrorsOverturnDoneDeclaration(t *testing.T) {
	calls := 0
	var feedbacks []GoalFeedback
	RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, feedback GoalFeedback, _ GoalLoopSummaries) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			if calls == 2 {
				return Result{
					ParseErrors: []*blocks.BlockParseError{
						{BlockKind: "change", Boundary: "甲子"},
					},
				}, nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !strings.Contains(string(feedbacks[2]), "malformed block(s)") {
		t.Fatalf("feedback after parse errors must carry the re-emit note, got %q", feedbacks[2])
	}
	// Loop 1 declares done, loop 2's parse errors overturn the
	// declaration, loop 3 declares again, loop 4 confirms.
	if calls != 4 {
		t.Fatalf("ran %d loops, want 4", calls)
	}
}

func TestRunGoalAggregatesStatsWithLoopNumbers(t *testing.T) {
	output := &bytes.Buffer{}
	result := RunGoal(context.Background(), GoalOptions{
		Output: output,
		Generate: func(ctx context.Context, feedback GoalFeedback, _ GoalLoopSummaries) (Result, []AttemptStat, error) {
			stats := []AttemptStat{{Attempt: 1, PromptTokens: 10}}
			return doneResult(), stats, nil
		},
		Review: noopReview,
	})
	if !result.Achieved {
		t.Fatal("goal must be achieved")
	}
	if len(result.Stats) != 2 {
		t.Fatalf("got %d stats, want 2", len(result.Stats))
	}
	if result.Stats[0].Loop != 1 || result.Stats[1].Loop != 2 {
		t.Fatalf("Loop fields = %d, %d; want 1, 2", result.Stats[0].Loop, result.Stats[1].Loop)
	}
	if !strings.Contains(output.String(), "Goal Loop Statistics") {
		t.Fatal("output must contain the aggregated statistics table")
	}
}

func TestGoalSystemPromptContent(t *testing.T) {
	if !strings.Contains(GoalSystemPrompt, "Goal-Directed Multi-Loop Execution") {
		t.Fatal("GoalSystemPrompt must contain the goal-directed multi-loop header")
	}
	if !strings.Contains(GoalSystemPrompt, `kind "done"`) {
		t.Fatal("GoalSystemPrompt must describe the done block kind")
	}
	if strings.Contains(GoalSystemPrompt, ".GOAL_COMPLETE") {
		t.Fatal("GoalSystemPrompt must not reference a marker file")
	}
	if strings.Contains(GoalSystemPrompt, "<<DELIMITER") {
		t.Fatal("GoalSystemPrompt must not display the literal template marker")
	}
}

func TestGoalSystemPromptText(t *testing.T) {
	prompt := string(GoalSystemPromptText(CodesComponents{}, "", nil))
	if !strings.HasPrefix(prompt, prompts.Codes) {
		t.Fatal("goal system prompt must start with the base codes prompt")
	}
	if !strings.Contains(prompt, "Goal-Directed Multi-Loop Execution") {
		t.Fatal("goal system prompt must contain the goal system prompt")
	}
	feedback := GoalFeedback("[System note: loop feedback]")
	prompt = string(GoalSystemPromptText(CodesComponents{}, feedback, nil))
	if !strings.HasSuffix(prompt, string(feedback)) {
		t.Fatal("feedback must be appended at the end of the system prompt")
	}
	summaries := GoalLoopSummaries{
		{Loop: 1, Text: "explored the parser"},
		{Loop: 2, Text: "fixed the boundary bug"},
	}
	prompt = string(GoalSystemPromptText(CodesComponents{}, "", summaries))
	if !strings.Contains(prompt, "- Loop 1: explored the parser") ||
		!strings.Contains(prompt, "- Loop 2: fixed the boundary bug") {
		t.Fatal("summaries must render as loop-tagged bullets")
	}
	// The summaries section is append-only: adding a loop extends the
	// section without altering earlier bytes, preserving the LLM prefix
	// cache across loops.
	if !strings.HasPrefix(
		GoalLoopSummaries{{Loop: 1, Text: "a"}, {Loop: 2, Text: "b"}}.SystemPromptSection(),
		GoalLoopSummaries{{Loop: 1, Text: "a"}}.SystemPromptSection(),
	) {
		t.Fatal("the summaries section must be append-only across loops")
	}
	// Summaries render before the feedback, so the append-only section
	// precedes the per-loop feedback.
	both := string(GoalSystemPromptText(CodesComponents{}, feedback, summaries))
	if !strings.Contains(both, "- Loop 2: fixed the boundary bug\n\n"+string(feedback)) {
		t.Fatal("summaries must render before the feedback")
	}
}

func TestGoalTheoryStatesNoProcessLevelCaches(t *testing.T) {
	if !strings.Contains(TheoryOfGoalMode, "no process-level caches") {
		t.Fatal("TheoryOfGoalMode must state that the pipeline holds no process-level caches")
	}
	if !strings.Contains(TheoryOfGoalMode, "scope provider functions") {
		t.Fatal("TheoryOfGoalMode must state that caches live inside scope provider functions")
	}
}
