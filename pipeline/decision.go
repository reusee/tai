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
  read_only_task — decides whether the change component stays out of the
  pipeline: only a task the model explicitly judges read-only omits the change
  block format, because such a task pays for a format it never uses and is
  invited by it to change files the user did not ask to change. Every other
  task keeps the change component — the task the model answers no, and the
  task no model judged at all.
- The criteria are independent. A task that only reviews or explains code is a
  programming task that is also read-only; a task that renames a configuration
  file or edits a document changes files without being a programming task.
- Only these two introductions depend on the judgment: the component sections,
  the generation pipeline, and every other prompt part are unchanged.
- The decision model's inputs are the user prompt, the session facts
  (TaskEnvironment: whether the session runs inside a Go module, the names of
  the working directory's top-level entries, plus every fact a later field
  adds), and the session's configured extra system prompts
  (flags.ExtraSystemPrompt, the extra_system_prompt config path). The entry
  names are the session's surface: files and directories alike, never the
  contents of its subdirectories, so the judgment reads what the task starts
  from without paying for file contents. The extra prompts often state the
  task's nature — its subject and its boundaries — so the judgment reads them
  with the task, exactly as the generation session runs on them. All three
  travel in the decision request: the task, the facts, and the extra
  instructions form its user message, and the judgment criteria are the
  declared functions' descriptions.
- The decision model currently always speaks the System One protocol: the
  decision declares the criterion functions and reads the model's noul answer
  to "does this function handle the latest user message?". The mechanism runs
  through the plain Generator contract, so any generator whose protocol answers
  the declared functions serves as the decision model.
- The judgment is made once per run and carried into every goal loop: the task
  does not change between loops, so re-judging could flip the introductions
  while the run unfolds. Module.GoalRun resolves it and forks it into each
  loop's scope; a non-goal command resolves it with its system prompt.
- The judgment fails open, and the two failures are not symmetric. With no
  decision model configured, an empty task, an unresolvable model, or a failed
  request, the programming criterion answers yes and the read-only criterion
  answers no: the base prompt and the change component stay included, because a
  faulty decision must not strip the coding rules, nor the ability to modify
  files, from a task that needs them. A false read-only answer withholds the
  change block format — the deliverable — while a false not-read-only answer
  costs tokens only. Every failure and every judgment are logged, so a decision
  model that never answers a declared question is visible.
- The judgment never overrides -no-apply: the change component's prompt stays
  included there for the dry run the user reviews, whatever the read-only
  answer is (see TheoryOfCodesComponents).
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

// TaskEnvironment carries the session facts and configured instructions the
// decision model receives besides the user prompt. A new fact is a new field;
// decisionInputText renders it into the decision request. See
// TheoryOfTaskDecision.
type TaskEnvironment struct {
	// InGoModule reports whether the session runs inside a Go module: the
	// nearest go.mod at or above the working directory, the same fact the
	// default-command auto-detection consults (see
	// cmd/tai.TheoryOfCommandAutoDetection).
	InGoModule bool

	// DirEntries carries the names of the working directory's top-level
	// entries — files and directories alike, sorted — and never the
	// contents of its subdirectories: the judgment reads the session's
	// surface, not the files under it. An unreadable directory contributes
	// nothing, so the fact degrades instead of failing the judgment. See
	// TheoryOfTaskDecision.
	DirEntries []string

	// ExtraSystemPrompt carries the user-configured extra instruction
	// sections (flags.ExtraSystemPrompt, the extra_system_prompt config
	// path). The sections often state the task's nature — its subject and
	// its boundaries — so the judgment reads them with the task, exactly as
	// the generation session runs on them. Empty sections contribute
	// nothing. See TheoryOfTaskDecision.
	ExtraSystemPrompt flags.ExtraSystemPrompt
}

// TaskEnvironment provider: the facts and configured instructions of the
// current session. See TheoryOfTaskDecision.
func (Module) TaskEnvironment(extra flags.ExtraSystemPrompt) TaskEnvironment {
	taskEnvironment := TaskEnvironment{ExtraSystemPrompt: extra}
	dir, err := os.Getwd()
	if err != nil {
		return taskEnvironment
	}
	_, inGoModule := pathutil.FindGoModuleRoot(dir)
	taskEnvironment.InGoModule = inGoModule
	taskEnvironment.DirEntries = dirEntryNames(dir)
	return taskEnvironment
}

// dirEntryNames returns the names of a directory's top-level entries, files
// and directories alike, in the sorted order os.ReadDir reports. It never
// descends, so the contents of the directory's subdirectories stay out of the
// decision request: a large tree under one entry costs the judgment nothing.
// An unreadable directory yields nil, so the fact degrades instead of failing
// the judgment. See TheoryOfTaskDecision.
func dirEntryNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
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
// session's two criteria. Programming reports whether the task is a programming
// task; ReadOnly reports whether the task is read-only — one that inspects,
// explains, or reviews files without modifying them. Both answers come from one
// decision request. See TheoryOfTaskDecision.
type TaskDecision struct {
	Programming bool
	ReadOnly    bool
}

// ProgrammingTask reports whether the session's task is a programming task, as
// judged by the decision model. The system prompt introduces the codes base
// prompt only for a programming task. See TheoryOfTaskDecision.
type ProgrammingTask bool

// ReadOnlyTask reports whether the session's task is read-only, as judged by
// the decision model. The codes pipeline omits the change component — the
// change block format and its processing — only for an explicit read-only
// judgment, so a task that inspects, explains, or reviews files pays nothing
// for a format it never uses. Every other task keeps the component, and a
// failed or skipped judgment keeps it too, because a false read-only answer
// would withhold the deliverable while a false not-read-only answer costs
// tokens only. See TheoryOfTaskDecision.
type ReadOnlyTask bool

// TaskDecision provider: it judges the session's task with the decision model —
// both criteria in one request — records the judgment's material in the scope's
// TaskDecisionSink for the session tree, and fails open on every failure, with
// the read-only criterion answering no so the change component stays included.
// dscope caches the provider per scope, so the judgment runs once however many
// consumers resolve the derived values. See TheoryOfTaskDecision.
func (Module) TaskDecision(
	taskEnvironment TaskEnvironment,
	chats flags.Chats,
	getDecisionGenerator GetDecisionGenerator,
	logger logs.Logger,
	decisionSink *TaskDecisionSink,
) TaskDecision {
	// keep is the fail-open answer: the codes base prompt stays included and
	// the read-only criterion answers no, so the change component stays
	// included when no judgment is made. See TheoryOfTaskDecision.
	keep := TaskDecision{Programming: true, ReadOnly: false}
	prompt := strings.TrimSpace(strings.Join(chats, "\n"))
	if prompt == "" {
		// No task to judge: the session runs no generation against an
		// empty task, so the decision is skipped and both introductions
		// stay included.
		decisionSink.Record(TaskDecisionRecord{
			Programming: keep.Programming,
			ReadOnly:    keep.ReadOnly,
			Note:        "no task to judge: the session has no chat input, so the codes base prompt and the change component stay included",
		})
		return keep
	}
	generator, err := getDecisionGenerator()
	if err != nil {
		decisionSink.Record(TaskDecisionRecord{
			Programming: keep.Programming,
			ReadOnly:    keep.ReadOnly,
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
			ReadOnly:    keep.ReadOnly,
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
		ReadOnly:    decision.ReadOnly,
	}
	if err != nil {
		record.Programming = keep.Programming
		record.ReadOnly = keep.ReadOnly
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
		"read_only", decision.ReadOnly,
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

// ReadOnlyTask provider: the read-only half of the run's task decision. See
// TheoryOfTaskDecision.
func (Module) ReadOnlyTask(decision TaskDecision) ReadOnlyTask {
	return ReadOnlyTask(decision.ReadOnly)
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

// decisionReadOnlyFuncName is the name of the function the decision request
// declares for the read-only criterion: the decision model selects it when the
// session's task is read-only, so the change component is omitted from the
// pipeline. See TheoryOfTaskDecision.
const decisionReadOnlyFuncName = "read_only_task"

// decisionReadOnlyFuncDescription is the criterion the decision request
// declares for the read-only answer. It states the artifact the task leaves
// unchanged, so a task that writes a file does not match it. See
// TheoryOfTaskDecision.
const decisionReadOnlyFuncDescription = "handles a read-only task: inspecting, explaining, or reviewing files without modifying them, and running no command that modifies the working tree"

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
				Name:        decisionReadOnlyFuncName,
				Description: decisionReadOnlyFuncDescription,
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
		ReadOnly:    decisionFuncSelected(decided, decisionReadOnlyFuncName),
	}, output, nil
}

// decisionInputText renders the decision request's user message: the task to
// judge, the session facts, and the session's configured extra instructions,
// each rendered verbatim. See TheoryOfTaskDecision.
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
	if len(taskEnvironment.DirEntries) > 0 {
		sb.WriteString("\nWorking directory entries:\n\n")
		for _, name := range taskEnvironment.DirEntries {
			sb.WriteString("- ")
			sb.WriteString(name)
			sb.WriteString("\n")
		}
	}
	var extras []string
	for _, section := range taskEnvironment.ExtraSystemPrompt {
		if section = strings.TrimSpace(section); section != "" {
			extras = append(extras, section)
		}
	}
	if len(extras) > 0 {
		sb.WriteString("\nExtra instructions:\n\n")
		sb.WriteString(strings.Join(extras, "\n\n"))
		sb.WriteString("\n")
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
