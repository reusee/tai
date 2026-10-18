package pipeline

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/records"
	"github.com/reusee/tai/tree"
)

// decisionRunScope builds a pipeline scope whose judgment is deterministic and
// whose recording is inert, so a test can resolve the task decision and run
// the loop. See TheoryOfTaskDecision.
func decisionRunScope(t *testing.T, generator *decisionTestGenerator) dscope.Scope {
	t.Helper()
	return dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() *records.Recorder { return nil },
		func() flags.Chats { return flags.Chats{"fix the parser bug"} },
		func() flags.DecisionModel { return flags.DecisionModel("decision-test") },
		func() TaskEnvironment { return TaskEnvironment{InGoModule: true} },
		func() generators.GetGenerator {
			return func(name string) (generators.Generator, error) {
				return generator, nil
			}
		},
	)
}

// TestTaskDecisionSinkRecordsJudgment verifies that the judgment records its
// material in the scope's sink: the decision model, the request's input, the
// model's output, and the verdict. A drained sink is empty, so one decision
// is written once. See TheoryOfTaskDecision.
func TestTaskDecisionSinkRecordsJudgment(t *testing.T) {
	generator := &decisionTestGenerator{programming: true}
	scope := decisionRunScope(t, generator)
	if !bool(scope.Get[ProgrammingTask]()) {
		t.Fatal("expected a programming task")
	}
	record := scope.Get[*TaskDecisionSink]().Drain()
	if record == nil {
		t.Fatal("expected a recorded task decision")
	}
	if record.Model != generator.Spec().Model {
		t.Fatalf("record model = %q, want the decision model %q", record.Model, generator.Spec().Model)
	}
	if !strings.Contains(record.Input, "fix the parser bug") {
		t.Fatalf("the record input must carry the task, got %q", record.Input)
	}
	if !strings.Contains(record.Output, decisionFuncName) {
		t.Fatalf("the record output must carry the model's selection, got %q", record.Output)
	}
	if record.Note != "" {
		t.Fatalf("a judged decision carries no fail-open note, got %q", record.Note)
	}
	if scope.Get[*TaskDecisionSink]().Drain() != nil {
		t.Fatal("a drained sink must be empty")
	}
}

// TestTaskDecisionSinkRecordsFailOpen verifies that a skipped judgment
// records its fail-open reason instead of the request's material, so the
// session tree states why the codes base prompt stayed included. See
// TheoryOfTaskDecision.
func TestTaskDecisionSinkRecordsFailOpen(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() flags.Chats { return flags.Chats{"write a haiku"} },
	)
	if !bool(scope.Get[ProgrammingTask]()) {
		t.Fatal("no decision model must keep the codes base prompt included")
	}
	record := scope.Get[*TaskDecisionSink]().Drain()
	if record == nil {
		t.Fatal("expected a recorded task decision")
	}
	if record.Note == "" {
		t.Fatal("a skipped judgment must carry its fail-open reason")
	}
	if record.Input != "" || record.Output != "" {
		t.Fatalf("a skipped judgment carries no request material, got input %q and output %q", record.Input, record.Output)
	}
}

// TestRunRecordsTaskDecisionNode verifies that a fresh run writes the task
// decision its scope recorded as a decision::task node under the session
// root, with the request's input, the model's output, and the verdict as
// child nodes. The judgment runs during prompt assembly, before the tree
// opens, so its material waits in the scope's sink. See TheoryOfTaskDecision.
func TestRunRecordsTaskDecisionNode(t *testing.T) {
	scope := decisionRunScope(t, &decisionTestGenerator{programming: true})
	// Resolving the programming-task provider is what prompt assembly does:
	// it fills the scope's decision sink before the loop opens its tree.
	scope.Get[ProgrammingTask]()
	result, err := runOnce(scope.Get[Run](), RunOptions{
		InitialState: generators.NewPrompts("", nil),
		PhaseBuilder: func(generators.Generator) generators.Phase {
			return appendPhase("hello\n")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes := result.SessionTree.ByType(tree.TypeDecisionTask)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 task decision node, got %d", len(nodes))
	}
	node := nodes[0]
	if node.Parent != "root" {
		t.Fatalf("task decision parent = %q, want root", node.Parent)
	}
	if !strings.Contains(node.Content, "task decision: programming task") {
		t.Fatalf("task decision content must state the decision, got %q", node.Content)
	}
	if !strings.Contains(node.Content, "codes base prompt: included") {
		t.Fatalf("task decision content must state the base prompt's fate, got %q", node.Content)
	}
	children := map[tree.Type]bool{}
	for _, child := range node.Children() {
		children[child.Type] = true
	}
	for _, typ := range []tree.Type{
		tree.TypeDecisionInput, tree.TypeDecisionOutput, tree.TypeDecisionVerdict,
	} {
		if !children[typ] {
			t.Fatalf("task decision must carry a %v child, got %v", typ, node.Children())
		}
	}
}

// TestRunGoalRecordsTaskDecisionNode verifies that a goal run writes the
// run's task decision once under the run tree's root, before its loops: the
// judgment is made once per run and forked into every loop, so it is a
// run-level record. See TheoryOfTaskDecision.
func TestRunGoalRecordsTaskDecisionNode(t *testing.T) {
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(context.Context, int, GoalFeedback, GoalLoopSummaries, string, SessionTreeContinuation) (Result, []AttemptStat, error) {
			return doneResult(), nil, nil
		},
		Review: noopReview,
		TaskDecision: &TaskDecisionRecord{
			Model:       "decision-test",
			Input:       "Task:\nfix the parser bug",
			Output:      "selected: programming_task",
			Programming: true,
		},
	})
	if !result.Achieved {
		t.Fatal("the run must be achieved")
	}
	nodes := result.Tree.ByType(tree.TypeDecisionTask)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 task decision node, got %d", len(nodes))
	}
	node := nodes[0]
	if node.Parent != "root" {
		t.Fatalf("task decision parent = %q, want root", node.Parent)
	}
	if len(result.Tree.ByType(tree.TypeLoop)) != 1 {
		t.Fatalf("expected 1 loop node, got %d", len(result.Tree.ByType(tree.TypeLoop)))
	}
	children := map[tree.Type]bool{}
	for _, child := range node.Children() {
		children[child.Type] = true
	}
	for _, typ := range []tree.Type{
		tree.TypeDecisionInput, tree.TypeDecisionOutput, tree.TypeDecisionVerdict,
	} {
		if !children[typ] {
			t.Fatalf("task decision must carry a %v child, got %v", typ, node.Children())
		}
	}
	// A run without a recorded decision writes no node.
	plain := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(context.Context, int, GoalFeedback, GoalLoopSummaries, string, SessionTreeContinuation) (Result, []AttemptStat, error) {
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if got := len(plain.Tree.ByType(tree.TypeDecisionTask)); got != 0 {
		t.Fatalf("a run without a task decision must write no node, got %d", got)
	}
}
