package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/prompts"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline/codetypes"
)

// decisionTestGenerator stands in for the decision model: it answers the
// decision request from fixed verdicts — appending the declared function
// calls when the task is a programming task and when it modifies files —
// and records the request's user text and declared functions so a test can
// check what the request carried. See TheoryOfTaskDecision.
type decisionTestGenerator struct {
	programming bool
	modifyFiles bool
	err         error
	seen        []string
	funcs       []generators.FuncDecl
}

var _ generators.Generator = (*decisionTestGenerator)(nil)

func (g *decisionTestGenerator) Spec() generators.Spec {
	return generators.Spec{
		Model: "decision-test",
	}
}

func (g *decisionTestGenerator) CountTokens(text string) (int, error) {
	return len(text), nil
}

func (g *decisionTestGenerator) Generate(
	ctx context.Context,
	state generators.State,
	options *generators.GenerateOptions,
) (generators.State, error) {
	if g.err != nil {
		return state, g.err
	}
	for content := range state.Contents() {
		for _, part := range content.Parts {
			if text, ok := part.(generators.Text); ok {
				g.seen = append(g.seen, string(text))
			}
		}
	}
	for fn := range state.Functions() {
		g.funcs = append(g.funcs, fn.Decl)
	}
	var calls []generators.Part
	if g.programming {
		calls = append(calls, generators.FuncCall{
			ID:        decisionProgrammingFuncName,
			Name:      decisionProgrammingFuncName,
			Arguments: map[string]any{},
		})
	}
	if g.modifyFiles {
		calls = append(calls, generators.FuncCall{
			ID:        decisionModifyFilesFuncName,
			Name:      decisionModifyFilesFuncName,
			Arguments: map[string]any{},
		})
	}
	if len(calls) == 0 {
		return state, nil
	}
	return state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: calls,
	})
}

// decisionTestScope builds a codes scope whose decision model is the given
// generator and whose session facts are fixed, so the judgment is
// deterministic. See TheoryOfTaskDecision.
func decisionTestScope(t *testing.T, generator *decisionTestGenerator, chats ...string) dscope.Scope {
	return dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() flags.Chats { return flags.Chats(chats) },
		func() flags.DecisionModel { return flags.DecisionModel("decision-test") },
		func() TaskEnvironment { return TaskEnvironment{InGoModule: true} },
		func() generators.GetGenerator {
			return func(name string) (generators.Generator, error) {
				return generator, nil
			}
		},
	)
}

func TestSystemPromptIntroducesCodesPromptOnlyForProgrammingTask(t *testing.T) {
	generator := &decisionTestGenerator{programming: false}
	prompt := string(decisionTestScope(t, generator, "write a haiku about the sea").Get[SystemPrompt]())
	if strings.Contains(prompt, prompts.Codes) {
		t.Fatal("a non-programming task must not introduce the codes base prompt")
	}

	// The decision request carries the task and the session facts.
	input := strings.Join(generator.seen, "\n")
	if !strings.Contains(input, "write a haiku about the sea") {
		t.Fatalf("the decision request must carry the task, got %q", input)
	}
	if !strings.Contains(input, "- inside a Go module: yes") {
		t.Fatalf("the decision request must carry the in-go-module fact, got %q", input)
	}

	generator.programming = true
	prompt = string(decisionTestScope(t, generator, "fix the parser bug").Get[SystemPrompt]())
	if !strings.Contains(prompt, prompts.Codes) {
		t.Fatal("a programming task must introduce the codes base prompt")
	}
}

func TestGoalSystemPromptIntroducesCodesPromptOnlyForProgrammingTask(t *testing.T) {
	without := string(GoalSystemPromptText(CodesComponents{}, "", nil, false))
	if strings.Contains(without, prompts.Codes) {
		t.Fatal("a non-programming task must not introduce the codes base prompt")
	}
	if !strings.Contains(without, "Goal-Directed Multi-Loop Execution") {
		t.Fatal("the goal system prompt must stay included")
	}
	with := string(GoalSystemPromptText(CodesComponents{}, "", nil, true))
	if !strings.HasPrefix(with, prompts.Codes) {
		t.Fatal("a programming task must introduce the codes base prompt")
	}
}

func TestProgrammingTaskFailsOpen(t *testing.T) {
	// No decision model configured: the codes prompt stays included.
	task := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() flags.Chats { return flags.Chats{"write a haiku"} },
	).Get[ProgrammingTask]()
	if !bool(task) {
		t.Fatal("no decision model must keep the codes prompt included")
	}

	// A failed decision keeps the codes prompt included.
	generator := &decisionTestGenerator{err: errors.New("decision model unavailable")}
	task = decisionTestScope(t, generator, "write a haiku").Get[ProgrammingTask]()
	if !bool(task) {
		t.Fatal("a failed decision must keep the codes prompt included")
	}
}

func TestDecisionInputTextCarriesSessionFacts(t *testing.T) {
	text := decisionInputText("do something", TaskEnvironment{InGoModule: false})
	if !strings.Contains(text, "do something") {
		t.Fatalf("the decision input must carry the task, got %q", text)
	}
	if !strings.Contains(text, "- inside a Go module: no") {
		t.Fatalf("the decision input must carry the in-go-module fact, got %q", text)
	}
}

// TestDecisionRequestCarriesCriterionFunction pins the declaration seam: each
// criterion must travel to the decision model as a declared function, because
// decisionFuncSelected reads a function call under the same name. A
// declaration that never arrives would leave the generator nothing to answer
// for that criterion and make its judgment fail open, silently keeping the
// introduction in every session. See TheoryOfTaskDecision.
func TestDecisionRequestCarriesCriterionFunction(t *testing.T) {
	generator := &decisionTestGenerator{programming: true, modifyFiles: true}
	decisionTestScope(t, generator, "fix the parser bug").Get[ProgrammingTask]()
	if len(generator.funcs) != 2 {
		t.Fatalf("the decision request must declare exactly two criterion functions, got %v", generator.funcs)
	}
	declared := map[string]bool{}
	for _, fn := range generator.funcs {
		if fn.Description == "" {
			t.Fatalf("the criterion function %q must carry the criterion description", fn.Name)
		}
		declared[fn.Name] = true
	}
	for _, name := range []string{decisionProgrammingFuncName, decisionModifyFilesFuncName} {
		if !declared[name] {
			t.Fatalf("the decision request must declare the criterion function %q, got %v", name, generator.funcs)
		}
	}
}

// TestModifyFilesTaskGatesChange pins the second criterion's gate: the change
// component — the change block format and its processing — enters the
// pipeline only when the decision model judges the task a file-modification
// task, so a read-only task pays nothing for the format and is not invited by
// it to change files the user did not ask to change. A task the model judged
// read-only sees the disabled-blocks notice instead, so its change blocks are
// never silently ignored. Under -no-apply the prompt stays included for the
// dry run the user reviews. See TheoryOfTaskDecision and
// TheoryOfCodesComponents.
func TestModifyFilesTaskGatesChange(t *testing.T) {
	// A read-only task: no change component, and the kind is announced as
	// disabled.
	scope := decisionTestScope(t, &decisionTestGenerator{programming: true}, "explain this parser")
	if bool(scope.Get[ModifyFilesTask]()) {
		t.Fatal("a read-only task must judge the file-modification criterion no")
	}
	prompt := string(scope.Get[SystemPrompt]())
	if strings.Contains(prompt, "Change Block Kind") {
		t.Fatal("a read-only task must not introduce the change block kind")
	}
	if !strings.Contains(prompt, "change blocks are not processed in this session") {
		t.Fatal("a read-only task must announce the disabled change kind")
	}

	// A file-modification task: the change component enters the prompt.
	scope = decisionTestScope(t, &decisionTestGenerator{programming: true, modifyFiles: true}, "fix the parser bug")
	if !bool(scope.Get[ModifyFilesTask]()) {
		t.Fatal("a file-modification task must judge the criterion yes")
	}
	prompt = string(scope.Get[SystemPrompt]())
	if !strings.Contains(prompt, "Change Block Kind") {
		t.Fatal("a file-modification task must introduce the change block kind")
	}
	if strings.Contains(prompt, "change blocks are not processed in this session") {
		t.Fatal("an available change kind must not be announced as disabled")
	}

	// Under -no-apply the change prompt stays included whatever the
	// criterion answered: the emitted blocks are the deliverable of the dry
	// run the user reviews.
	scope = decisionTestScope(t, &decisionTestGenerator{programming: true}, "explain this parser").Fork(
		func() flags.Apply { return false },
	)
	prompt = string(scope.Get[SystemPrompt]())
	if !strings.Contains(prompt, "Change Block Kind") {
		t.Fatal("under -no-apply the change prompt stays included")
	}
	if strings.Contains(prompt, "change blocks are not processed in this session") {
		t.Fatal("under -no-apply change must not be announced as disabled")
	}
}
