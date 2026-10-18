package pipeline

import (
	"fmt"
	"strings"
	"sync"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/tree"
)

// TaskDecisionRecord carries one run's task decision for the session tree:
// what the decision model was asked, what it produced, and what was decided.
// It is the material of the decision::task node and its input, output, and
// verdict children. See TheoryOfTaskDecision.
type TaskDecisionRecord struct {
	// Model is the decision model's identity; empty when no decision model
	// is configured or the model could not resolve.
	Model string
	// Input is the decision request's user message; empty when no request
	// was formed.
	Input string
	// Output is the decision model's produced output; empty when the
	// request produced none.
	Output string
	// Note is the fail-open reason when no judgment was made: a skipped
	// judgment or a failed request. Empty when the request ran and
	// answered.
	Note string
	// Programming is the programming-task criterion's answer: the codes
	// base prompt enters the system prompt when it is true. It is true on
	// every failure, because the judgment fails open.
	Programming bool
	// ModifyFiles is the file-modification criterion's answer: the change
	// component enters the pipeline when it is true. It is true on every
	// failure, because the judgment fails open.
	ModifyFiles bool
}

// TaskDecisionSink buffers one scope's task decision for the session tree:
// the judgment runs during prompt assembly, before the session tree opens
// (see TheoryOfTaskDecision), so Module.ProgrammingTask records the
// decision's material here and the tree's owner writes it as a decision::task
// node — Module.Run for a fresh run, RunGoal for a goal run. The sink is a
// pointer value provided per scope: dscope caches the provider result, so the
// judgment and the loop of one scope share one sink, and a goal run's scope
// reset installs a fresh sink per loop. All methods are nil-receiver safe, so
// call sites need no nil checks.
type TaskDecisionSink struct {
	mu     sync.Mutex
	record *TaskDecisionRecord
}

// Record stores the decision's material, replacing any earlier record: one
// scope holds at most one run's decision.
func (s *TaskDecisionSink) Record(record TaskDecisionRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record = &record
}

// Drain returns the buffered decision and empties the sink, so a
// drain-and-write pass never writes the task decision twice.
func (s *TaskDecisionSink) Drain() *TaskDecisionRecord {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.record
	s.record = nil
	return record
}

// TaskDecisionSink provides the per-scope buffer of the run's task decision.
// See TheoryOfTaskDecision.
func (Module) TaskDecisionSink() *TaskDecisionSink {
	return &TaskDecisionSink{}
}

// renderDecisionOutput renders the decision model's produced output: the
// contents the request's generation appended to its state. Text is taken
// verbatim, and the other material carries a one-line marker with its kind,
// so the record reads as plain text: a thought is the model's reasoning, a
// function call its selection, a call result the criterion function's result,
// and a usage the request's token spend, which no attempt statistic covers
// because the judgment runs outside the generation state. See
// TheoryOfTaskDecision.
func renderDecisionOutput(state generators.State, sinceCount int) string {
	if state == nil {
		return ""
	}
	var sb strings.Builder
	index := 0
	for content := range state.Contents() {
		if index >= sinceCount {
			for _, part := range content.Parts {
				switch p := part.(type) {
				case generators.Text:
					sb.WriteString(string(p))
				case generators.Thought:
					sb.WriteString("thinking: " + string(p) + "\n")
				case generators.FuncCall:
					sb.WriteString("selected: " + p.Name + "\n")
				case generators.CallResult:
					sb.WriteString("result: " + p.Name + "\n")
				case generators.Usage:
					fmt.Fprintf(&sb,
						"usage: prompt %d (cached %d), completion %d, thoughts %d\n",
						p.Prompt.TokenCount, p.Prompt.TokenCountCached,
						p.Candidates.TokenCount, p.Thoughts.TokenCount,
					)
				}
			}
		}
		index++
	}
	return strings.TrimRight(sb.String(), "\n")
}

// writeTaskDecisionNode writes one run's task decision as a decision::task
// node under parent: the node's content states the answers, the decision
// model, and the fates of the codes base prompt and the change component, and
// its children carry the request's input, the model's output, and the verdict
// with its basis or its fail-open reason. An input or output that never
// existed writes no child, so a decision that never ran carries only its
// verdict. A failed write leaves the tree unchanged. See TheoryOfTaskDecision.
func writeTaskDecisionNode(tr *tree.Tree, parent string, record *TaskDecisionRecord) *tree.Tree {
	if tr == nil || record == nil {
		return tr
	}
	answers := "no judgment"
	if record.Note == "" {
		answers = fmt.Sprintf("programming task: %s, modifies files: %s",
			yesNo(record.Programming), yesNo(record.ModifyFiles))
	}
	model := record.Model
	if model == "" {
		model = "none"
	}
	base := "omitted"
	if record.Programming {
		base = "included"
	}
	change := "omitted"
	if record.ModifyFiles {
		change = "included"
	}
	next, taskName, err := tr.WriteAuto(
		parent, tree.TypeDecisionTask.Name(), tree.TypeDecisionTask, tree.AuthorProgram,
		fmt.Sprintf("task decision: %s\nmodel: %s\ncodes base prompt: %s\nchange component: %s",
			answers, model, base, change),
	)
	if err != nil {
		return tr
	}
	writeChild := func(typ tree.Type, content string) {
		if content == "" {
			return
		}
		if written, _, err := next.WriteAuto(taskName, typ.Name(), typ, tree.AuthorProgram, content); err == nil {
			next = written
		}
	}
	if record.Input != "" {
		var sb strings.Builder
		sb.WriteString("system prompt:\n")
		sb.WriteString(decisionSystemPrompt)
		sb.WriteString("\n\ncriteria:\n- " + decisionProgrammingFuncName + ": " + decisionProgrammingFuncDescription)
		sb.WriteString("\n- " + decisionModifyFilesFuncName + ": " + decisionModifyFilesFuncDescription)
		sb.WriteString("\n\nuser message:\n")
		sb.WriteString(record.Input)
		writeChild(tree.TypeDecisionInput, sb.String())
	}
	writeChild(tree.TypeDecisionOutput, record.Output)
	verdict := record.Note
	if verdict == "" {
		verdict = fmt.Sprintf(
			"the decision model answered the criteria %q (%s) and %q (%s): the codes base prompt is %s and the change component is %s",
			decisionProgrammingFuncName, yesNo(record.Programming),
			decisionModifyFilesFuncName, yesNo(record.ModifyFiles),
			base, change,
		)
	}
	writeChild(tree.TypeDecisionVerdict, verdict)
	return next
}

// yesNo renders one criterion's answer for the decision node's content and
// verdict. See TheoryOfTaskDecision.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
