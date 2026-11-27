package selfprompts

import (
	"context"
	"errors"
	"strings"

	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/pathutil"
)

// NewComponent returns the self-prompt block component: its prompt teaches
// the kind, and its Process applies one round's batch to the self-prompt file
// of dir. dryRun validates and reports without writing, so a run that must
// not touch the working tree keeps the batch reviewable and changes nothing.
// See TheoryOfSelfPrompt.
func NewComponent(dir pathutil.TaiDir, dryRun bool) components.Component {
	return components.Component{
		Kind:          Kind,
		PromptSection: SelfPromptBlockPrompt,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			return processBatch(dir, pctx, !dryRun)
		},
	}
}

// NewContentComponent returns the prompt-only component that renders the
// stored directives into the system prompt. It reads the file of dir at
// construction time — once per goal loop, once per session — which is the
// mechanism that keeps an update out of the running loop. The caller places
// it last, so the sections above stay in the LLM prefix cache when an entry
// changes. See TheoryOfSelfPrompt.
func NewContentComponent(dir pathutil.TaiDir) components.Component {
	return components.Component{
		PromptSection: Content(dir),
	}
}

// processBatch applies one round's blocks and builds the feedback that
// becomes the next round's user content: a rejected batch lists every
// problem so the model corrects it, an applied batch reports the entries and
// reminds the model that this session's system prompt is unchanged. The
// batch lands in dir, the tai dir the scope resolved. See
// TheoryOfSelfPrompt.
func processBatch(
	dir pathutil.TaiDir,
	pctx *components.ProcessContext,
	write bool,
) components.ProcessResult {
	ops, err := Apply(dir, pctx.Blocks, write)
	var rejected *RejectedError
	if errors.As(err, &rejected) {
		return components.ProcessResult{
			Parts: []generators.Part{generators.Text(rejectionFeedback(rejected))},
		}
	}
	if err != nil {
		return components.ProcessResult{Err: err}
	}
	return components.ProcessResult{
		Parts: []generators.Part{generators.Text(appliedFeedback(ops, write))},
	}
}

// rejectionFeedback reports a discarded batch: nothing was written, and the
// listed problems are what the model corrects before re-emitting.
func rejectionFeedback(rejected *RejectedError) string {
	var sb strings.Builder
	sb.WriteString("[self-prompt] The batch was rejected and nothing was written:\n")
	for _, problem := range rejected.Problems {
		sb.WriteString("- ")
		sb.WriteString(problem.Error())
		sb.WriteString("\n")
	}
	sb.WriteString("Correct the batch and re-emit it.\n\n")
	return sb.String()
}

// appliedFeedback reports a validated batch. The timing note is the point:
// the running session's system prompt keeps the entries it started with.
func appliedFeedback(ops []Operation, write bool) string {
	names := make([]string, 0, len(ops))
	for _, op := range ops {
		names = append(names, op.Name+" ("+op.Op+")")
	}
	var sb strings.Builder
	sb.WriteString("[self-prompt] ")
	if write {
		sb.WriteString("Wrote .tai/" + selfPromptFile + " in the tai dir: " + strings.Join(names, ", ") + ".\n")
		sb.WriteString("This session's system prompt is unchanged: the update takes effect in the next session or the next goal loop.\n\n")
		return sb.String()
	}
	sb.WriteString("The batch is valid; this run is a dry run, so nothing was written: " + strings.Join(names, ", ") + ".\n")
	sb.WriteString("The entries reach the file when a run with file writes enabled applies them.\n\n")
	return sb.String()
}
