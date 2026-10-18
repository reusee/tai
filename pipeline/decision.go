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
- One decision request answers two criteria about the session's task, so the
  session pays one cheap call. The decision model declares each criterion as a
  function and, per function, answers whether it handles the latest user
  message: a selected function is a yes.
- The first criterion — programming_task — decides whether the codes base
  prompt (prompts.Codes) enters the system prompt: a session whose task is not
  a programming task pays for every coding rule it never applies. The second —
  modify_files_task — decides whether the change component enters the pipeline:
  a task that modifies no file pays for the change block format and is invited
  by it to change files the user did not ask to change.
- The criteria are independent. A task that only reviews or explains code is a
  programming task without a file modification; a task that renames a
  configuration file or edits a document modifies files without being a
  programming task.
- Only these two introductions depend on the judgment: the component sections,
  the generation pipeline, and every other prompt part are unchanged.
- The decision model's inputs are the user prompt and the session facts
  (TaskEnvironment: whether the session runs inside a Go module, plus every
  fact a later field adds). They travel in the decision request: the task and
  the facts form its user message, and the judgment criteria are the declared
  functions' descriptions.
- The decision model currently always speaks the System One protocol: the
  decision declares the criterion functions and reads the model's noul answer
  to "does this function handle the latest user message?". The mechanism runs
  through the plain Generator contract, so any generator whose protocol answers
  the declared functions serves as the decision model.
- The judgment is made once per run and carried into every goal loop: the task
  does not change between loops, so re-judging could flip the introductions
  while the run unfolds. Module.GoalRun resolves it and forks it into each
  loop's scope; a non-goal command resolves it with its system prompt.
- The judgment fails open: with no decision model configured, an empty task, an
  unresolvable model, or a failed request, both criteria answer yes — the base
  prompt and the change component stay included — because a faulty decision
  must not strip the coding rules, nor the ability to modify files, from a task
  that needs them. Every failure and every judgment are logged, so a decision
  model that never answers a declared question is visible.
- The judgment never overrides -no-apply: the change component's prompt stays
  included there for the dry run the user reviews, whatever the
  file-modification answer is (see TheoryOfCodesComponents).
- The decision request runs outside the generation state, so its token spend is
  not part of the attempt statistics; the generator's own log records it.
- The decision joins the session tree as a run-level record: one
  decision::task node — the answers, the decision model, and the fates of the
  codes base prompt and the change component — whose input, output, and verdict
  children carry the request's input, the model's produced output, and the
  verdict with its basis. The judgment runs during prompt assembly, before the
  session tree opens, so Module.TaskDecision buffers the record in the scope's
  TaskDecisionSink and the tree's owner writes it: a fresh run writes it under
  the session root when Module.Run opens the tree, and a goal run writes it
  once under the run tree's root before the first loop, because the judgment is
  made once per run and forked into every loop.
- The record is written whether or not a judgment was made: a skipped or failed
  judgment carries the fail-open reason in its verdict, so the tree states why
  the base prompt and the change component stayed included.
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

// TaskDecision is one run's task decision: the decision model's answers to the
// session's two criteria. Programming reports whether the task is a
// programming task; ModifyFiles reports whether the task modifies project
// files. Both answers come from one decision request. See TheoryOfTaskDecision.
type TaskDecision struct {
	Programming bool
	ModifyFiles bool
}

// ProgrammingTask reports whether the session's task is a programming task, as
// judged by the decision model. The system prompt introduces the codes base
// prompt only for a programming task. See TheoryOfTaskDecision.
type ProgrammingTask bool

// ModifyFilesTask reports whether the session's task modifies project files, as
// judged by the decision model. The codes pipeline includes the change
// component — the change block format and its processing — only for such a
// task, so a read-only task pays nothing for the format and is not invited by
// it to change files the user did not ask to change. See TheoryOfTaskDecision.
type ModifyFilesTask bool

// TaskDecision provider: it judges the session's task with the decision model —
// both criteria in one request — records the judgment's material in the scope's
// TaskDecisionSink for the session tree, and fails open on every failure.
// dscope caches the provider per scope, so the judgment runs once however many
// consumers resolve the derived values. See TheoryOfTaskDecision.
func (Module) TaskDecision(
	taskEnvironment TaskEnvironment,
	chats flags.Chats,
	getDecisionGenerator GetDecisionGenerator,
	logger logs.Logger,
	decisionSink *TaskDecisionSink,
) TaskDecision {
	// keep is the fail-open answer: the codes base prompt and the change
	// component stay included when no judgment is made.
	keep := TaskDecision{Programming: true, ModifyFiles: true}
	prompt := strings.TrimSpace(strings.Join(chats, "\n"))
	if prompt == "" {
		// No task to judge: the session runs no generation against an
		// empty task, so the decision is skipped and both introductions
		// stay included.
		decisionSink.Record(TaskDecisionRecord{
			Programming: keep.Programming,
			ModifyFiles: keep.ModifyFiles,
			Note:        "no task to judge: the session has no chat input, so the codes base prompt and the change component stay included",
		})
		return keep
	}
	generator, err := getDecisionGenerator()
	if err != nil {
		decisionSink.Record(TaskDecisionRecord{
			Programming: keep.Programming,
			ModifyFiles: keep.ModifyFiles,
			Note:        "the decision model did not resolve: " + err.Error() + "; the codes base prompt and the change component stay included",
		})
		logger.Warn("the decision model is unavailable; the codes prompt and the change component stay included",
			"error", err,
		)
		return keep
	}
	if generator == nil {
		// No decision model is configured: keep both introductions as
		// before.
		decisionSink.Record(TaskDecisionRecord{
			Programming: keep.Programming,
			ModifyFiles: keep.ModifyFiles,
			Note:        "no decision model is configured; the codes base prompt and the change component stay included",
		})
		return keep
	}
	// The judgment runs during prompt assembly, outside any generation
	// context, so it carries its own background context.
	decision, output, err := judgeTask(
		context.Background(), generator, prompt, taskEnvironment,
	)
	record := TaskDecisionRecord{
		Model:       generator.Spec().Model,
		Input:       decisionInputText(prompt, taskEnvironment),
		Output:      output,
		Programming: decision.Programming,
		ModifyFiles: decision.ModifyFiles,
	}
	if err != nil {
		record.Programming = keep.Programming
		record.ModifyFiles = keep.ModifyFiles
		record.Note = "the decision request failed: " + err.Error() + "; the codes base prompt and the change component stay included"
		decisionSink.Record(record)
		logger.Warn("the task decision failed; the codes prompt and the change component stay included",
			"model", generator.Spec().Model,
			"error", err,
		)
		return keep
	}
	decisionSink.Record(record)
	logger.Info("task decision",
		"programming", decision.Programming,
		"modify_files", decision.ModifyFiles,
		"model", generator.Spec().Model,
		"in_go_module", taskEnvironment.InGoModule,
	)
	return decision
}

// ProgrammingTask provider: the programming-task half of the run's task
// decision. See TheoryOfTaskDecision.
func (Module) ProgrammingTask(decision TaskDecision) ProgrammingTask {
	return ProgrammingTask(decision.Programming)
}

// ModifyFilesTask provider: the file-modification half of the run's task
// decision. See TheoryOfTaskDecision.
func (Module) ModifyFilesTask(decision TaskDecision) ModifyFilesTask {
	return ModifyFilesTask(decision.ModifyFiles)
}

// decisionProgrammingFuncName is the name of the function the decision request
// declares for the programming-task criterion: the decision model selects it
// when the session's task is a programming task. See TheoryOfTaskDecision.
const decisionProgrammingFuncName = "programming_task"

// decisionProgrammingFuncDescription is the criterion the decision request
// declares for the programming-task answer. The System One protocol asks
// whether the declared function handles the latest user message, so the
// description reads as that capability. See TheoryOfTaskDecision.
const decisionProgrammingFuncDescription = "handles a programming task: writing, modifying, reviewing, or exploring code, or running builds and tests on a codebase"

// decisionModifyFilesFuncName is the name of the function the decision request
// declares for the file-modification criterion: the decision model selects it
// when the session's task modifies project files. See TheoryOfTaskDecision.
const decisionModifyFilesFuncName = "modify_files_task"

// decisionModifyFilesFuncDescription is the criterion the decision request
// declares for the file-modification answer. It states the artifact the task
// writes, so a task that only reads, explains, or reviews a file does not
// match it. See TheoryOfTaskDecision.
const decisionModifyFilesFuncDescription = "handles a task that modifies files: creating, editing, renaming, or deleting a project file, or applying a patch or diff to the working tree"

// decisionSystemPrompt frames the decision request. The task and its session
// facts form the request's user message and the criteria are the declared
// functions, so the system prompt states only the frame. See
// TheoryOfTaskDecision.
const decisionSystemPrompt = "Classify the session task stated in the user message, given the session facts that follow it. Each declared function states one judgment criterion: select every function whose criterion the task matches, and select no function for a criterion the task does not match. Answer each declared question."

// judgeTask asks the decision model both task criteria in one request: it
// declares the criteria as functions over a one-message state, reads the
// answers from the selected functions, and renders the model's produced output
// for the session tree. See TheoryOfTaskDecision.
func judgeTask(
	ctx context.Context,
	generator generators.Generator,
	prompt string,
	taskEnvironment TaskEnvironment,
) (
	decision TaskDecision,
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
	state = generators.NewFuncMap(state,
		&generators.Function{
			Decl: generators.FuncDecl{
				Name:        decisionProgrammingFuncName,
				Description: decisionProgrammingFuncDescription,
			},
		},
		&generators.Function{
			Decl: generators.FuncDecl{
				Name:        decisionModifyFilesFuncName,
				Description: decisionModifyFilesFuncDescription,
			},
		},
	)
	baseCount := generators.CountContents(state)
	decided, err := generator.Generate(ctx, state, nil)
	// The generator returns the partial state with its error, so the
	// rendered output carries what the model produced before the failure.
	output = renderDecisionOutput(decided, baseCount)
	if err != nil {
		return TaskDecision{}, output, err
	}
	return TaskDecision{
		Programming: decisionFuncSelected(decided, decisionProgrammingFuncName),
		ModifyFiles: decisionFuncSelected(decided, decisionModifyFilesFuncName),
	}, output, nil
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

// decisionFuncSelected reports whether the decision request selected one
// declared function: the generator appends a FuncCall part under the declared
// name when the decision model answers that criterion yes. See
// TheoryOfTaskDecision.
func decisionFuncSelected(state generators.State, name string) bool {
	for content := range state.Contents() {
		for _, part := range content.Parts {
			call, ok := part.(generators.FuncCall)
			if !ok {
				continue
			}
			if call.Name == name {
				return true
			}
		}
	}
	return false
}
