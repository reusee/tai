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
// calls when the task is a programming task and when it is read-only —
// and records the request's user text and declared functions so a test can
// check what the request carried. See TheoryOfTaskDecision.
type decisionTestGenerator struct {
	programming bool
	readOnly    bool
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
	if g.readOnly {
		calls = append(calls, generators.FuncCall{
			ID:        decisionReadOnlyFuncName,
			Name:      decisionReadOnlyFuncName,
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

// TestDecisionRequestCarriesExtraSystemPrompt pins the wiring: the session's
// configured extra system prompts reach the decision request's input, so the
// judgment reads the instructions the generation session runs on instead of
// judging the task from the prompt alone. The scope resolves the module
// provider — the helper scope forks the facts directly — so the injection the
// session runs is the one under test. See TheoryOfTaskDecision.
func TestDecisionRequestCarriesExtraSystemPrompt(t *testing.T) {
	generator := &decisionTestGenerator{programming: true}
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() flags.Chats { return flags.Chats{"explain the parser"} },
		func() flags.DecisionModel { return flags.DecisionModel("decision-test") },
		func() flags.ExtraSystemPrompt {
			return flags.ExtraSystemPrompt{"reply in Chinese"}
		},
		func() generators.GetGenerator {
			return func(name string) (generators.Generator, error) {
				return generator, nil
			}
		},
	)
	scope.Get[TaskDecision]()
	record := scope.Get[*TaskDecisionSink]().Drain()
	if record == nil {
		t.Fatal("expected a recorded task decision")
	}
	if !strings.Contains(record.Input, "reply in Chinese") {
		t.Fatalf("the decision request's input must carry the extra system prompt, got %q", record.Input)
	}
}

// TestDecisionInputTextCarriesExtraSystemPrompt pins the third decision input:
// the session's configured extra system prompt sections travel with the task,
// so the judgment reads the instructions the generation session runs on. The
// section is labeled, and blank entries render nothing, so an unconfigured
// session's request stays byte-identical. See TheoryOfTaskDecision.
func TestDecisionInputTextCarriesExtraSystemPrompt(t *testing.T) {
	text := decisionInputText("do something", TaskEnvironment{
		ExtraSystemPrompt: flags.ExtraSystemPrompt{
			"this is a documentation task",
			"",
			"   ",
		},
	})
	if !strings.Contains(text, "Extra instructions:") {
		t.Fatalf("the decision input must label the extra instructions, got %q", text)
	}
	if !strings.Contains(text, "this is a documentation task") {
		t.Fatalf("the decision input must carry the extra prompt, got %q", text)
	}
	if strings.Contains(text, "\n\n\n\n") {
		t.Fatalf("blank extra prompts must not widen the section, got %q", text)
	}
	for _, taskEnvironment := range []TaskEnvironment{
		{},
		{ExtraSystemPrompt: flags.ExtraSystemPrompt{"", "   "}},
	} {
		got := decisionInputText("do something", taskEnvironment)
		if strings.Contains(got, "Extra instructions:") {
			t.Fatalf("an unconfigured session must render no extra section, got %q", got)
		}
	}
}

// TestDecisionRequestCarriesCriterionFunction pins the declaration seam: each
// criterion must travel to the decision model as a declared function, because
// decisionFuncSelected reads a function call under the same name. A
// declaration that never arrives would leave the generator nothing to answer
// for that criterion and make its judgment fail open, silently keeping the
// introduction in every session. See TheoryOfTaskDecision.
func TestDecisionRequestCarriesCriterionFunction(t *testing.T) {
	generator := &decisionTestGenerator{programming: true}
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
	for _, name := range []string{decisionProgrammingFuncName, decisionReadOnlyFuncName} {
		if !declared[name] {
			t.Fatalf("the decision request must declare the criterion function %q, got %v", name, generator.funcs)
		}
	}
}

// TestReadOnlyTaskGatesChange pins the second criterion's gate: the change
// component — the change block format and its processing — stays in the
// pipeline unless the decision model explicitly judges the task read-only,
// because only a task that inspects, explains, or reviews files without
// changing them pays for a format it never uses and is invited by it to
// change files the user did not ask to change. Every other answer, including
// no answer at all, keeps the component: a false read-only answer would
// withhold the deliverable, while a false not-read-only answer costs tokens
// only. A task the model judged read-only sees the disabled-blocks notice
// instead, so its change blocks are never silently ignored. Under -no-apply
// the prompt stays included for the dry run the user reviews. See
// TheoryOfTaskDecision and TheoryOfCodesComponents.
func TestReadOnlyTaskGatesChange(t *testing.T) {
	// An explicit read-only task: no change component, and the kind is
	// announced as disabled.
	scope := decisionTestScope(t, &decisionTestGenerator{programming: true, readOnly: true}, "explain this parser")
	if !bool(scope.Get[ReadOnlyTask]()) {
		t.Fatal("a read-only task must judge the read-only criterion yes")
	}
	prompt := string(scope.Get[SystemPrompt]())
	if strings.Contains(prompt, "Change Block Kind") {
		t.Fatal("a read-only task must not introduce the change block kind")
	}
	if !strings.Contains(prompt, "change blocks are not processed in this session") {
		t.Fatal("a read-only task must announce the disabled change kind")
	}

	// Any other task — here, the model did not select the read-only
	// criterion — keeps the change component: the gate omits the component
	// only on an explicit read-only judgment.
	scope = decisionTestScope(t, &decisionTestGenerator{programming: true}, "fix the parser bug")
	if bool(scope.Get[ReadOnlyTask]()) {
		t.Fatal("a task the model did not judge read-only must keep the change component")
	}
	prompt = string(scope.Get[SystemPrompt]())
	if !strings.Contains(prompt, "Change Block Kind") {
		t.Fatal("a not-read-only task must introduce the change block kind")
	}
	if strings.Contains(prompt, "change blocks are not processed in this session") {
		t.Fatal("an available change kind must not be announced as disabled")
	}

	// Under -no-apply the change prompt stays included whatever the
	// criterion answered: the emitted blocks are the deliverable of the dry
	// run the user reviews.
	scope = decisionTestScope(t, &decisionTestGenerator{programming: true, readOnly: true}, "explain this parser").Fork(
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
