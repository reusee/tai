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
// decision request from a fixed verdict — appending the declared function
// call when the task is a programming task — and records the request's user
// text and declared functions so a test can check what the request carried.
// See TheoryOfTaskDecision.
type decisionTestGenerator struct {
	programming bool
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
	if !g.programming {
		return state, nil
	}
	return state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.FuncCall{
				ID:        decisionFuncName,
				Name:      decisionFuncName,
				Arguments: map[string]any{},
			},
		},
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

// TestDecisionRequestCarriesCriterionFunction pins the declaration seam: the
// criterion must travel to the decision model as a declared function, because
// decisionSelected reads a function call under the same name. A declaration
// that never arrives would leave the generator nothing to answer and make
// every judgment fail open, silently keeping the codes prompt in every
// session. See TheoryOfTaskDecision.
func TestDecisionRequestCarriesCriterionFunction(t *testing.T) {
	generator := &decisionTestGenerator{programming: true}
	decisionTestScope(t, generator, "fix the parser bug").Get[ProgrammingTask]()
	if len(generator.funcs) != 1 {
		t.Fatalf("the decision request must declare exactly one criterion function, got %v", generator.funcs)
	}
	if generator.funcs[0].Name != decisionFuncName {
		t.Fatalf("the criterion function must be declared as %q, got %q", decisionFuncName, generator.funcs[0].Name)
	}
	if generator.funcs[0].Description == "" {
		t.Fatal("the criterion function must carry the criterion description")
	}
}
