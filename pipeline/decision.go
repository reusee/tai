package pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/pathutil"
)

const TheoryOfTaskDecision = `
task decision theory:
- The codes base prompt (prompts.Codes) carries the coding workflow: change
  blocks, test runs, codebase exploration. A session whose task is not a
  programming task pays for every rule it never applies, so the base prompt
  enters the system prompt only when the decision model judges the task a
  programming task. Only the base prompt's introduction depends on the
  judgment: the component sections, the generation pipeline, and every other
  prompt part are unchanged.
- The decision model's inputs are the user prompt and the session facts
  (TaskEnvironment: whether the session runs inside a Go module, plus every
  fact a later field adds). They travel in the decision request: the task and
  the facts form its user message, and the judgment criterion is the declared
  function's description.
- The decision model currently always speaks the System One protocol: the
  decision declares one function — the programming-task criterion — and reads
  the model's noul answer to "does this function handle the latest user
  message?". A selected function is a programming task; no selection is not.
  The mechanism runs through the plain Generator contract, so any generator
  whose protocol answers the declared function serves as the decision model.
- The judgment is made once per run and carried into every goal loop: the task
  does not change between loops, so re-judging could flip the base prompt while
  the run unfolds. Module.GoalRun resolves it and forks it into each loop's
  scope; a non-goal command resolves it with its system prompt.
- The judgment fails open: with no decision model configured, an empty task,
  an unresolvable model, or a failed request, the base prompt stays included —
  a faulty decision must not strip the coding rules from a task that needs
  them. Every failure and every judgment are logged, so a decision model that
  never answers the declared question is visible.
- The decision request runs outside the generation state, so its token spend
  is not part of the attempt statistics; the generator's own log records it.
- The decision joins the session tree as a run-level record: one
  decision::task node — the decision, the decision model, and the codes base
  prompt's fate — whose input, output, and verdict children carry the
  request's input, the model's produced output, and the verdict with its
  basis. The judgment runs during prompt assembly, before the session tree
  opens, so Module.ProgrammingTask buffers the record in the scope's
  TaskDecisionSink and the tree's owner writes it: a fresh run writes it
  under the session root when Module.Run opens the tree, and a goal run
  writes it once under the run tree's root before the first loop, because
  the judgment is made once per run and forked into every loop.
- The record is written whether or not a judgment was made: a skipped or
  failed judgment carries the fail-open reason in its verdict, so the tree
  states why the codes base prompt stayed included.
`

// TaskEnvironment carries the session facts the decision model receives
// besides the user prompt. A new fact is a new field; decisionInputText
// renders it into the decision request. See TheoryOfTaskDecision.
type TaskEnvironment struct {
	// InGoModule reports whether the session runs inside a Go module: the
	// nearest go.mod at or above the working directory, the same fact the
	// default-command auto-detection consults (see
	// cmd/tai.TheoryOfCommandAutoDetection).
	InGoModule bool
}

// TaskEnvironment provider: the facts of the current session. See
// TheoryOfTaskDecision.
func (Module) TaskEnvironment() TaskEnvironment {
	dir, err := os.Getwd()
	if err != nil {
		return TaskEnvironment{}
	}
	_, inGoModule := pathutil.FindGoModuleRoot(dir)
	return TaskEnvironment{
		InGoModule: inGoModule,
	}
}

// GetDecisionGenerator resolves the decision model of the session: the
// generator named by flags.DecisionModel, or nil when no decision model is
// configured. A resolution failure is returned as an error, and the judgment
// fails open on it. See TheoryOfTaskDecision.
type GetDecisionGenerator func() (generators.Generator, error)

// GetDecisionGenerator provider. See TheoryOfTaskDecision.
func (Module) GetDecisionGenerator(
	decisionModel flags.DecisionModel,
	reset dscope.Reset,
) GetDecisionGenerator {
	return func() (generators.Generator, error) {
		name := strings.TrimSpace(string(decisionModel))
		if name == "" {
			return nil, nil
		}
		// The generator resolves lazily, from a scope reset at call time:
		// no decision model means no generator graph is resolved, and a
		// fork of generators.GetGenerator takes effect.
		getGenerator := reset().Get[generators.GetGenerator]()
		generator, err := getGenerator(name)
		if err != nil {
			return nil, fmt.Errorf("resolving the decision model %q: %w", name, err)
		}
		return generator, nil
	}
}

// ProgrammingTask reports whether the session's task is a programming task,
// as judged by the decision model. The system prompt introduces the codes
// base prompt only for a programming task. See TheoryOfTaskDecision.
type ProgrammingTask bool

// ProgrammingTask provider: it judges the session's task with the decision
// model, records the judgment's material in the scope's TaskDecisionSink for
// the session tree, and fails open on every failure. See TheoryOfTaskDecision.
func (Module) ProgrammingTask(
	taskEnvironment TaskEnvironment,
	chats flags.Chats,
	getDecisionGenerator GetDecisionGenerator,
	logger logs.Logger,
	decisionSink *TaskDecisionSink,
) ProgrammingTask {
	prompt := strings.TrimSpace(strings.Join(chats, "\n"))
	if prompt == "" {
		// No task to judge: the session runs no generation against an
		// empty task, so the decision is skipped and the base prompt
		// stays included.
		decisionSink.Record(TaskDecisionRecord{
			Programming: true,
			Note:        "no task to judge: the session has no chat input, so the codes base prompt stays included",
		})
		return true
	}
	generator, err := getDecisionGenerator()
	if err != nil {
		decisionSink.Record(TaskDecisionRecord{
			Programming: true,
			Note:        "the decision model did not resolve: " + err.Error() + "; the codes base prompt stays included",
		})
		logger.Warn("the decision model is unavailable; the codes prompt stays included",
			"error", err,
		)
		return true
	}
	if generator == nil {
		// No decision model is configured: keep the base prompt as before.
		decisionSink.Record(TaskDecisionRecord{
			Programming: true,
			Note:        "no decision model is configured; the codes base prompt stays included",
		})
		return true
	}
	// The judgment runs during prompt assembly, outside any generation
	// context, so it carries its own background context.
	isProgramming, output, err := judgeProgrammingTask(
		context.Background(), generator, prompt, taskEnvironment,
	)
	record := TaskDecisionRecord{
		Model:       generator.Spec().Model,
		Input:       decisionInputText(prompt, taskEnvironment),
		Output:      output,
		Programming: isProgramming,
	}
	if err != nil {
		record.Programming = true
		record.Note = "the decision request failed: " + err.Error() + "; the codes base prompt stays included"
		decisionSink.Record(record)
		logger.Warn("the task decision failed; the codes prompt stays included",
			"model", generator.Spec().Model,
			"error", err,
		)
		return true
	}
	decisionSink.Record(record)
	logger.Info("task decision",
		"programming", isProgramming,
		"model", generator.Spec().Model,
		"in_go_module", taskEnvironment.InGoModule,
	)
	return ProgrammingTask(isProgramming)
}

// decisionFuncName is the name of the function the decision request declares:
// the decision model selects it when the session's task is a programming
// task. See TheoryOfTaskDecision.
const decisionFuncName = "programming_task"

// decisionFuncDescription is the criterion the decision request declares: the
// capability a programming task requires. The System One protocol asks
// whether the declared function handles the latest user message, so the
// description reads as that capability. See TheoryOfTaskDecision.
const decisionFuncDescription = "handles a programming task: writing, modifying, reviewing, or exploring code, or running builds and tests on a codebase"

// decisionSystemPrompt frames the decision request. The task and its session
// facts form the request's user message and the criterion is the declared
// function, so the system prompt states only the frame. See
// TheoryOfTaskDecision.
const decisionSystemPrompt = "Classify the session task stated in the user message, given the session facts that follow it. The declared function states the criterion: select it when the task matches it. Answer the declared question."

// judgeProgrammingTask asks the decision model whether the prompt is a
// programming task: it declares the criterion as one function over a
// one-message state, reads the answer from the selected functions, and
// renders the model's produced output for the session tree. See
// TheoryOfTaskDecision.
func judgeProgrammingTask(
	ctx context.Context,
	generator generators.Generator,
	prompt string,
	taskEnvironment TaskEnvironment,
) (
	programming bool,
	output string,
	err error,
) {
	var state generators.State
	state = generators.NewPrompts(decisionSystemPrompt, []*generators.Content{
		{
			Role: generators.RoleUser,
			Parts: []generators.Part{
				generators.Text(decisionInputText(prompt, taskEnvironment)),
			},
		},
	})
	state = generators.NewFuncMap(state, &generators.Function{
		Decl: generators.FuncDecl{
			Name:        decisionFuncName,
			Description: decisionFuncDescription,
		},
	})
	baseCount := generators.CountContents(state)
	decided, err := generator.Generate(ctx, state, nil)
	// The generator returns the partial state with its error, so the
	// rendered output carries what the model produced before the failure.
	output = renderDecisionOutput(decided, baseCount)
	if err != nil {
		return false, output, err
	}
	return decisionSelected(decided), output, nil
}

// decisionInputText renders the decision request's user message: the task to
// judge, followed by the session facts. See TheoryOfTaskDecision.
func decisionInputText(prompt string, taskEnvironment TaskEnvironment) string {
	var sb strings.Builder
	sb.WriteString("Task:\n")
	sb.WriteString(prompt)
	sb.WriteString("\n\nSession facts:\n")
	if taskEnvironment.InGoModule {
		sb.WriteString("- inside a Go module: yes\n")
	} else {
		sb.WriteString("- inside a Go module: no\n")
	}
	return sb.String()
}

// decisionSelected reports whether the decision request selected the declared
// function: the generator appends a FuncCall part under the declared name when
// the decision model judges the task a programming task. See
// TheoryOfTaskDecision.
func decisionSelected(state generators.State) bool {
	for content := range state.Contents() {
		for _, part := range content.Parts {
			call, ok := part.(generators.FuncCall)
			if !ok {
				continue
			}
			if call.Name == decisionFuncName {
				return true
			}
		}
	}
	return false
}
