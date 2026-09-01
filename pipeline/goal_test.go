package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
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

// doneWithChangesResult builds a Result that declares done while carrying
// applied changes, so the declaring loop does not end the run under the
// done-without-changes rule and instead enters verification. See
// TheoryOfGoalMode.
func doneWithChangesResult() Result {
	ret := doneResult()
	ret.Diffs = []changes.FileDiff{{Path: "a.go"}}
	return ret
}

// noopReview is a review stub for goal tests.
func noopReview(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
	return nil
}

func TestRunGoalConfirmsDoneAfterVerificationLoop(t *testing.T) {
	output := &bytes.Buffer{}
	calls := 0
	var feedbacks []GoalFeedback
	var reviewModels []string
	result := RunGoal(context.Background(), GoalOptions{
		Output:       output,
		ReviewModels: []string{"review-model"},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, reviewModel string) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			reviewModels = append(reviewModels, reviewModel)
			if calls == 1 {
				// The declaring loop carries applied changes; a done
				// block without changes would end the run directly. See
				// TheoryOfGoalMode.
				return doneWithChangesResult(), nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !result.Achieved {
		t.Fatal("goal must be achieved when the verification loop emits a change-free done block")
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
	if reviewModels[0] != "" {
		t.Fatalf("first loop review model = %q, want empty: the declaring loop runs on the default model", reviewModels[0])
	}
	if reviewModels[1] != "review-model" {
		t.Fatalf("verification loop review model = %q, want \"review-model\"", reviewModels[1])
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
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
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

func TestRunGoalCarriesPreviousLoopSummaries(t *testing.T) {
	calls := 0
	var summaries []GoalLoopSummaries
	RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, s GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			summaries = append(summaries, s)
			if calls == 1 {
				return Result{}, []AttemptStat{
					{Attempt: 1, Summary: "explored the parser"},
					{Attempt: 2},
					{Attempt: 3, Summary: "fixed the boundary bug"},
				}, errors.New("api down")
			}
			if calls == 2 {
				// The declaring loop carries applied changes; a done
				// block without changes would end the run directly. See
				// TheoryOfGoalMode.
				return doneWithChangesResult(), nil, nil
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
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
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

func TestRunGoalDiskChangeHandoffFeedsNextLoop(t *testing.T) {
	calls := 0
	var feedbacks []GoalFeedback
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			switch calls {
			case 1, 2:
				return Result{}, nil, &DiskChangeHandoffError{
					Err:     &changes.DiskChangedError{Path: "a.go"},
					Handoff: &Handoff{Prompt: "handoff notes from the interrupted loop"},
				}
			case 3:
				return Result{}, nil, errors.New("boom")
			case 4:
				// The declaring loop carries applied changes; a done
				// block without changes would end the run directly. See
				// TheoryOfGoalMode.
				return doneWithChangesResult(), nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !result.Achieved {
		t.Fatal("goal should be achieved")
	}
	// The disk-change handoff must not count toward the consecutive-error
	// bound: two handoffs plus one ordinary error leave room for the run
	// to continue and confirm the goal.
	if calls != 5 {
		t.Fatalf("expected 5 loops (2 handoffs, 1 error, done + verification), got %d", calls)
	}
	if !strings.Contains(string(feedbacks[1]), "a.go") || !strings.Contains(string(feedbacks[1]), "handoff notes") {
		t.Fatalf("the handoff feedback must carry the disk change and the handoff notes, got %q", feedbacks[1])
	}
	if !strings.Contains(string(feedbacks[3]), "boom") {
		t.Fatalf("the ordinary error must still feed back, got %q", feedbacks[3])
	}
}

func TestRunGoalParseErrorsOverturnDoneDeclaration(t *testing.T) {
	calls := 0
	var feedbacks []GoalFeedback
	RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			if calls == 2 {
				return Result{
					ParseErrors: []*blocks.BlockParseError{
						{BlockKind: "change", Boundary: "甲子"},
					},
				}, nil, nil
			}
			if calls == 4 {
				// The change-free done block ends the run.
				return doneResult(), nil, nil
			}
			// The declaring loops carry applied changes; a done block
			// without changes would end the run directly. See
			// TheoryOfGoalMode.
			return doneWithChangesResult(), nil, nil
		},
		Review: noopReview,
	})
	if !strings.Contains(string(feedbacks[2]), "malformed block(s)") {
		t.Fatalf("feedback after parse errors must carry the re-emit note, got %q", feedbacks[2])
	}
	// Loop 1 declares done with changes, loop 2's parse errors force a
	// re-emit before any completion, loop 3 declares again with
	// changes, loop 4 emits the change-free done block that ends the
	// run.
	if calls != 4 {
		t.Fatalf("ran %d loops, want 4", calls)
	}
}

// TestRunGoalChangeFreeLoopWithoutDoneContinues verifies the corrected
// termination rule: a loop that applied no change blocks and emitted no
// done block is a model output failure, not a terminal state — the run
// continues with corrective feedback into a fresh loop, and the done
// block remains the only run exit. See TheoryOfGoalMode.
func TestRunGoalChangeFreeLoopWithoutDoneContinues(t *testing.T) {
	calls := 0
	var feedbacks []GoalFeedback
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			if calls == 1 {
				// The silent loop: no changes, no done block.
				return Result{}, nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if calls != 2 {
		t.Fatalf("ran %d loops, want 2: the silent loop must continue into the next loop", calls)
	}
	if !result.Achieved {
		t.Fatal("the run must end achieved on the later change-free done block")
	}
	if !strings.Contains(string(feedbacks[1]), "NO change blocks and NO done block") {
		t.Fatalf("the loop after the silent loop must carry the model-output-failure feedback, got %q", feedbacks[1])
	}
}

// TestRunGoalVerifiesCorrectionsUntilChangeFreeDone verifies the full
// verify-and-correct cycle: a done declaration with changes sends the
// next loop into verification; the verification loop's corrections are
// ordinary change blocks, so the loop after them continues normally
// with cleared feedback; the run ends when a loop verifies the current
// state, finds nothing to correct, and emits a change-free done block.
// See TheoryOfGoalMode.
func TestRunGoalVerifiesCorrectionsUntilChangeFreeDone(t *testing.T) {
	calls := 0
	var feedbacks []GoalFeedback
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			switch calls {
			case 1:
				return doneWithChangesResult(), nil, nil
			case 2:
				// The verification loop corrects an error without
				// declaring done.
				return Result{Diffs: []changes.FileDiff{{Path: "a.go"}}}, nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !result.Achieved {
		t.Fatal("goal must be achieved after the corrections are verified")
	}
	if result.LoopsRun != 3 {
		t.Fatalf("ran %d loops, want 3", result.LoopsRun)
	}
	if !strings.Contains(string(feedbacks[1]), "Verification is the primary work") {
		t.Fatalf("the loop after a done-with-changes loop must carry the verification prompt, got %q", feedbacks[1])
	}
	if feedbacks[2] != "" {
		t.Fatalf("the loop after a corrections-only loop must carry no feedback, got %q", feedbacks[2])
	}
}

// TestRunGoalDoneWithoutChangesEndsRun verifies the done-without-changes
// rule: a done block emitted by a loop that applied no change blocks ends
// the run in that loop. The declaring loop just read the current
// filesystem state through a fresh scope and concluded without changes,
// so a verification loop would only repeat the same analysis. See
// TheoryOfGoalMode.
func TestRunGoalDoneWithoutChangesEndsRun(t *testing.T) {
	output := &bytes.Buffer{}
	calls := 0
	result := RunGoal(context.Background(), GoalOptions{
		Output: output,
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if calls != 1 {
		t.Fatalf("ran %d loops, want 1: a done block without applied changes must not trigger a verification loop", calls)
	}
	if result.LoopsRun != 1 {
		t.Fatalf("LoopsRun = %d, want 1", result.LoopsRun)
	}
	if !result.Achieved {
		t.Fatal("the done block must mark the goal achieved")
	}
	if !strings.Contains(output.String(), "Goal Achieved") {
		t.Fatal("output must report the achieved goal")
	}
}

// TestRunGoalDoneWithChangesDoesNotEndRun verifies the core completion
// rule of the done mechanism: a done block emitted together with change
// blocks never ends the run — there is no confirmation-by-repetition.
// Each change-applying loop, done or not, is followed by a loop that
// reads the resulting filesystem state; the run ends only when a loop
// emits a done block and applies no change blocks. See TheoryOfGoalMode.
func TestRunGoalDoneWithChangesDoesNotEndRun(t *testing.T) {
	calls := 0
	var feedbacks []GoalFeedback
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			if calls < 3 {
				// Every loop applies changes and declares done; none of
				// them may end the run.
				return doneWithChangesResult(), nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !result.Achieved {
		t.Fatal("goal must be achieved by the final change-free done loop")
	}
	if result.LoopsRun != 3 {
		t.Fatalf("ran %d loops, want 3: a done block with changes never ends the run", result.LoopsRun)
	}
	for i := 1; i < len(feedbacks); i++ {
		if !strings.Contains(string(feedbacks[i]), "Verification is the primary work") {
			t.Fatalf("loop %d feedback must carry the verification prompt, got %q", i+1, feedbacks[i])
		}
	}
}

func TestRunGoalContinuesWhenLoopAppliesChanges(t *testing.T) {
	calls := 0
	RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			if calls == 1 {
				return Result{
					Diffs: []changes.FileDiff{{Path: "a.go"}},
				}, nil, nil
			}
			if calls == 2 {
				// The declaring loop carries applied changes; a done
				// block without changes would end the run directly. See
				// TheoryOfGoalMode.
				return doneWithChangesResult(), nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	// Loop 1 applies changes and continues; loop 2 declares done; loop
	// 3 confirms.
	if calls != 3 {
		t.Fatalf("ran %d loops, want 3", calls)
	}
}

// TestRunGoalSilentVerificationLoopContinues verifies that a silent loop
// does not end the run even when it follows a done declaration: the
// declaration stays pending (the silent loop verified nothing), the run
// continues with corrective feedback, and a later change-free done block
// achieves the goal. See TheoryOfGoalMode.
func TestRunGoalSilentVerificationLoopContinues(t *testing.T) {
	calls := 0
	var feedbacks []GoalFeedback
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			switch calls {
			case 1:
				// The declaring loop carries applied changes; a done
				// block without changes would end the run directly. See
				// TheoryOfGoalMode.
				return doneWithChangesResult(), nil, nil
			case 2:
				// The verification loop goes silent: no changes, no
				// done block — a model output failure.
				return Result{}, nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if calls != 3 {
		t.Fatalf("ran %d loops, want 3: the silent verification loop must continue", calls)
	}
	if !result.Achieved {
		t.Fatal("the run must end achieved on the later change-free done block")
	}
	if !strings.Contains(string(feedbacks[1]), "Verification is the primary work") {
		t.Fatalf("the loop after the declaration must carry the verification prompt, got %q", feedbacks[1])
	}
	if !strings.Contains(string(feedbacks[2]), "NO change blocks and NO done block") {
		t.Fatalf("the loop after the silent verification loop must carry the model-output-failure feedback, got %q", feedbacks[2])
	}
}

func TestRunGoalAggregatesStatsWithLoopNumbers(t *testing.T) {
	calls := 0
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			calls++
			stats := []AttemptStat{{Attempt: 1, PromptTokens: 10}}
			if calls == 1 {
				// The declaring loop carries applied changes; a done
				// block without changes would end the run directly. See
				// TheoryOfGoalMode.
				return doneWithChangesResult(), stats, nil
			}
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
}

func TestRunGoalReportsEventsThroughObserver(t *testing.T) {
	output := &bytes.Buffer{}
	var events []Event
	result := RunGoal(context.Background(), GoalOptions{
		Output: output,
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string) (Result, []AttemptStat, error) {
			return doneResult(), []AttemptStat{{Attempt: 1, PromptTokens: 10}}, nil
		},
		Review: noopReview,
		GoalEvents: func(ev Event) {
			events = append(events, ev)
		},
	})
	if !result.Achieved {
		t.Fatal("goal must be achieved")
	}
	// A done block emitted by a loop that applied no change blocks
	// achieves the goal directly: the achieved verdict is the only goal
	// event.
	var kinds []EventKind
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
	}
	wantKinds := []EventKind{EventGoal}
	if !slices.Equal(kinds, wantKinds) {
		t.Fatalf("expected event kinds %v, got %v", wantKinds, kinds)
	}
	if !strings.Contains(events[0].Detail, "Goal Achieved") {
		t.Fatalf("expected the achieved verdict as an event, got %q", events[0].Detail)
	}
	if output.Len() != 0 {
		t.Fatalf("expected the output writer to stay empty when the observer is set, got %q", output.String())
	}
}

// TestRunGoalReviewModelStickyAfterOverturnedDeclaration verifies that
// the review-model switch is sticky: after the first done block, the
// loops that carry later corrections — including those following a loop
// whose parse errors forced a re-emit — keep running on the review
// model. See TheoryOfGoalReviewModel.
func TestRunGoalReviewModelStickyAfterOverturnedDeclaration(t *testing.T) {
	calls := 0
	var reviewModels []string
	RunGoal(context.Background(), GoalOptions{
		Output:       &bytes.Buffer{},
		ReviewModels: []string{"review-model"},
		Generate: func(ctx context.Context, _ int, _ GoalFeedback, _ GoalLoopSummaries, reviewModel string) (Result, []AttemptStat, error) {
			calls++
			reviewModels = append(reviewModels, reviewModel)
			if calls == 2 {
				return Result{
					ParseErrors: []*blocks.BlockParseError{
						{BlockKind: "change", Boundary: "甲子"},
					},
				}, nil, nil
			}
			if calls == 4 {
				// The change-free done block ends the run.
				return doneResult(), nil, nil
			}
			// The declaring loops carry applied changes; a done block
			// without changes would end the run directly. See
			// TheoryOfGoalMode.
			return doneWithChangesResult(), nil, nil
		},
		Review: noopReview,
	})
	// Loop 1 declares done with changes, loop 2's parse errors force a
	// re-emit, loop 3 declares again with changes, loop 4 emits the
	// change-free done block that ends the run.
	if calls != 4 {
		t.Fatalf("ran %d loops, want 4", calls)
	}
	if reviewModels[0] != "" {
		t.Fatalf("first loop review model = %q, want empty", reviewModels[0])
	}
	for i, model := range reviewModels[1:] {
		if model != "review-model" {
			t.Fatalf("loop %d review model = %q, want \"review-model\" (the switch is sticky)", i+2, model)
		}
	}
}

// TestRunGoalRotatesReviewModelsOnEachDoneBlock verifies the
// review-model rotation across the post-done loops: each done block
// emitted by a loop advances the selection by one, the last configured
// model is fixed once reached, and a loop without a done block does
// not advance it. See TheoryOfGoalReviewModel.
func TestRunGoalRotatesReviewModelsOnEachDoneBlock(t *testing.T) {
	calls := 0
	var reviewModels []string
	result := RunGoal(context.Background(), GoalOptions{
		Output:       &bytes.Buffer{},
		ReviewModels: []string{"review-a", "review-b"},
		Generate: func(ctx context.Context, _ int, _ GoalFeedback, _ GoalLoopSummaries, reviewModel string) (Result, []AttemptStat, error) {
			calls++
			reviewModels = append(reviewModels, reviewModel)
			switch calls {
			case 1:
				// First declaration: the next loop runs on review-a.
				return doneWithChangesResult(), nil, nil
			case 2:
				// Corrections without a done block: the selection
				// does not advance, so the next loop stays on
				// review-a.
				return Result{Diffs: []changes.FileDiff{{Path: "a.go"}}}, nil, nil
			case 3:
				// Second declaration: the next loop runs on review-b.
				return doneWithChangesResult(), nil, nil
			case 4:
				// Third declaration: the last model is fixed, so the
				// next loop stays on review-b.
				return doneWithChangesResult(), nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !result.Achieved {
		t.Fatal("goal must be achieved by the final change-free done loop")
	}
	if calls != 5 {
		t.Fatalf("ran %d loops, want 5", calls)
	}
	want := []string{"", "review-a", "review-a", "review-b", "review-b"}
	if !slices.Equal(reviewModels, want) {
		t.Fatalf("review models = %v, want %v", reviewModels, want)
	}
}

// TestRunGoalPostDoneLoopsKeepDefaultModelWithoutReviewModel verifies
// that post-done loops keep the default model when no review model is
// configured. See TheoryOfGoalReviewModel.
func TestRunGoalPostDoneLoopsKeepDefaultModelWithoutReviewModel(t *testing.T) {
	calls := 0
	var reviewModels []string
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, _ GoalFeedback, _ GoalLoopSummaries, reviewModel string) (Result, []AttemptStat, error) {
			reviewModels = append(reviewModels, reviewModel)
			calls++
			if calls == 1 {
				// The declaring loop carries applied changes; a done
				// block without changes would end the run directly. See
				// TheoryOfGoalMode.
				return doneWithChangesResult(), nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !result.Achieved {
		t.Fatal("goal must be achieved when the verification loop emits a change-free done block")
	}
	if result.LoopsRun != 2 {
		t.Fatalf("ran %d loops, want 2", result.LoopsRun)
	}
	for i, model := range reviewModels {
		if model != "" {
			t.Fatalf("loop %d review model = %q, want empty (no review model configured)", i+1, model)
		}
	}
}

func TestGoalSystemPromptContent(t *testing.T) {
	if !strings.Contains(GoalSystemPrompt, "Goal-Directed Multi-Loop Execution") {
		t.Fatal("GoalSystemPrompt must contain the goal-directed multi-loop header")
	}
	if !strings.Contains(GoalSystemPrompt, `kind "done"`) {
		t.Fatal("GoalSystemPrompt must describe the done block kind")
	}
	if !strings.Contains(GoalSystemPrompt, "without applying any change block") {
		t.Fatal("GoalSystemPrompt must address a loop that ends without applying any change block")
	}
	if !strings.Contains(GoalSystemPrompt, "does NOT end the run") {
		t.Fatal("GoalSystemPrompt must state that a change-free loop without a done block continues the run")
	}
	if !strings.Contains(GoalSystemPrompt, "applies no change blocks") {
		t.Fatal("GoalSystemPrompt must state the change-free done termination rule")
	}
	if !strings.Contains(GoalSystemPrompt, "gap analysis") {
		t.Fatal("GoalSystemPrompt must require a gap analysis before the done block")
	}
	if !strings.Contains(GoalSystemPrompt, "what was NOT done") {
		t.Fatal("GoalSystemPrompt must require checking what was not done against the original goal")
	}
	if strings.Contains(GoalSystemPrompt, "second consecutive loop") {
		t.Fatal("GoalSystemPrompt must not describe the removed two-done confirmation")
	}
	if strings.Contains(GoalSystemPrompt, ".GOAL_COMPLETE") {
		t.Fatal("GoalSystemPrompt must not reference a marker file")
	}
	if strings.Contains(GoalSystemPrompt, "<<DELIMITER") {
		t.Fatal("GoalSystemPrompt must not display the literal template marker")
	}
}

func TestGoalSystemPromptEconomizesRounds(t *testing.T) {
	if !strings.Contains(GoalSystemPrompt, "Economize rounds") {
		t.Fatal("goal prompt must teach round economy: batch context fetches and emit change blocks with their go-test verification in one response")
	}
}

func TestGoalDoneVerificationPromptContent(t *testing.T) {
	if !strings.Contains(goalDoneVerificationPrompt, "what was NOT done") {
		t.Fatal("the verification prompt must require checking what was not done")
	}
	if !strings.Contains(goalDoneVerificationPrompt, "gap") {
		t.Fatal("the verification prompt must frame verification as a gap analysis")
	}
	if !strings.Contains(goalDoneVerificationPrompt, "original goal") {
		t.Fatal("the verification prompt must compare the original goal against the current state")
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
