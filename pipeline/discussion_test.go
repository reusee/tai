package pipeline

import (
	"bytes"
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/reusee/prompts"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/tree"
)

// TestDiscussSystemPromptText pins the discussion system prompt: it is
// not the goal prompt — no rule about goal loops or the done block as a
// completion signal reaches the conversation — and it still introduces
// the codes base prompt for a programming task, matching the goal
// prompt's gating. See TheoryOfGoalDiscussion.
func TestDiscussSystemPromptText(t *testing.T) {
	prompt := string(DiscussSystemPromptText(CodesComponents{}, true))
	if !strings.HasPrefix(prompt, prompts.Codes) {
		t.Fatal("the discussion prompt must start with the base codes prompt for a programming task")
	}
	if !strings.Contains(prompt, "Post-Goal Discussion") {
		t.Fatal("the discussion prompt must carry the discussion section")
	}
	if strings.Contains(prompt, "Goal-Directed Multi-Loop Execution") {
		t.Fatal("the discussion prompt must not carry the goal system prompt")
	}
	if strings.Contains(prompt, "Goal Completion Signal") {
		t.Fatal("the discussion prompt must not teach the goal completion signal")
	}

	// A non-programming task pays nothing for the coding base prompt.
	prompt = string(DiscussSystemPromptText(CodesComponents{}, false))
	if strings.HasPrefix(prompt, prompts.Codes) {
		t.Fatal("a non-programming discussion must omit the base codes prompt")
	}
	if !strings.Contains(prompt, "Post-Goal Discussion") {
		t.Fatal("the discussion section appears for every task")
	}
}

// TestDiscussRunDecorator pins the decorator's two effects: the plan
// tree stops driving rounds, because the user does, and the interactive
// idle handler is installed so each user line opens one more round. See
// TheoryOfGoalDiscussion.
func TestDiscussRunDecorator(t *testing.T) {
	var captured RunOptions
	base := Run(func(ctx context.Context, opts RunOptions, result *Result) iter.Seq2[*tree.Tree, error] {
		captured = opts
		return func(yield func(*tree.Tree, error) bool) {}
	})
	idle := IdleHandler(func(ctx context.Context, state generators.State) (generators.State, bool, error) {
		return state, false, nil
	})
	build := func(generators.Generator, *generators.GenerateOptions) IdleHandler {
		return idle
	}
	run := discussRunDecorator(build)(base)
	for range run(context.Background(), RunOptions{
		PlanMode:  true,
		Generator: &restateThresholdGenerator{},
	}, &Result{}) {
	}
	if captured.PlanMode {
		t.Fatal("the discussion must not drive plan rounds")
	}
	if captured.OnIdle == nil {
		t.Fatal("the discussion must install the interactive idle handler")
	}
	// An existing handler — a command that already reads input — is
	// never replaced.
	existing := IdleHandler(func(ctx context.Context, state generators.State) (generators.State, bool, error) {
		return state, true, nil
	})
	for range discussRunDecorator(build)(base)(context.Background(), RunOptions{
		Generator: &restateThresholdGenerator{},
		OnIdle:    existing,
	}, &Result{}) {
	}
	if captured.OnIdle == nil {
		t.Fatal("the existing idle handler must stay installed")
	}
	if _, cont, err := captured.OnIdle(context.Background(), nil); err != nil || !cont {
		t.Fatalf("the existing idle handler must survive the decorator, got cont=%v err=%v", cont, err)
	}
}

// TestRunGoalRunsDiscussionAfterAchieved pins the discussion's place in
// the run: it starts after an achieved run, hangs under the last
// achieved loop node, and its nodes join the run tree. See
// TheoryOfGoalDiscussion.
func TestRunGoalRunsDiscussionAfterAchieved(t *testing.T) {
	output := &bytes.Buffer{}
	calls := 0
	discussed := false
	var parent string
	var gotTree *tree.Tree
	result := RunGoal(context.Background(), GoalOptions{
		Output: output,
		Generate: func(ctx context.Context, _ int, _ GoalFeedback, _ GoalLoopSummaries, _ string, _ SessionTreeContinuation) (Result, []AttemptStat, error) {
			calls++
			return doneResult(), nil, nil
		},
		Review: noopReview,
		Discussion: func(ctx context.Context, tr *tree.Tree, p string) (*tree.Tree, error) {
			discussed = true
			parent = p
			gotTree = tr
			// The discussion writes a node under the loop node it was
			// handed; the test checks the run tree carries it, so the
			// node's name is fixed rather than auto-generated. See
			// TheoryOfGoalDiscussion.
			next, err := tr.Write(p, "discussion", tree.TypeModel, tree.AuthorModel, "answer")
			if err != nil {
				return tr, err
			}
			return next, nil
		},
	})
	if !discussed {
		t.Fatal("an achieved run must start the discussion")
	}
	if !result.Discussed {
		t.Fatal("GoalResult must report the discussion")
	}
	if parent != "loop-1" {
		t.Fatalf("discussion parent = %q, want the last loop node %q", parent, "loop-1")
	}
	if gotTree == nil {
		t.Fatal("the discussion must receive the run tree")
	}
	if _, ok := result.Tree.Node("discussion"); !ok {
		t.Fatal("the discussion's nodes must join the run tree")
	}
	// The discussion is not another goal loop: the loop count stays at
	// the loop that declared the goal achieved.
	if calls != 1 || result.LoopsRun != 1 {
		t.Fatalf("%d loop(s) with LoopsRun %d, want exactly 1", calls, result.LoopsRun)
	}
}

// TestRunGoalDiscussionSkipsUnachievedRun pins the gate: a run that ends
// without a change-free done block starts no discussion, because it
// would present unfinished work as final. See TheoryOfGoalDiscussion.
func TestRunGoalDiscussionSkipsUnachievedRun(t *testing.T) {
	output := &bytes.Buffer{}
	discussed := false
	result := RunGoal(context.Background(), GoalOptions{
		Output: output,
		Generate: func(ctx context.Context, _ int, _ GoalFeedback, _ GoalLoopSummaries, _ string, _ SessionTreeContinuation) (Result, []AttemptStat, error) {
			return Result{}, nil, ErrNoTask
		},
		Review: noopReview,
		Discussion: func(ctx context.Context, tr *tree.Tree, parent string) (*tree.Tree, error) {
			discussed = true
			return tr, nil
		},
	})
	if discussed {
		t.Fatal("an unachieved run must start no discussion")
	}
	if result.Discussed {
		t.Fatal("GoalResult must not report a discussion")
	}
	if result.Achieved {
		t.Fatal("the no-task run is not achieved")
	}
}
