package pipeline

import (
	"fmt"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

const TheoryOfUnknownBlockKinds = `
Unknown-block-kind correction extends the parse-error self-correction
mechanism from malformed blocks to well-formed blocks whose kind the
session cannot process: a kind no component declares, a kind disabled
by configuration, or a kindless block. Such a block used to be silently
ignored — never executed, applied, or answered — while its output
implied an action that never happened. The loop now reports it
immediately after the attempt completes and feeds back a correction
error naming each block's kind and boundary, requiring the model to NOT
re-emit it (the kind itself is the fault: use the replacement behavior
the disabled-blocks notice states, or deliver the content as plain
text) and then continue the original task, ending with a summary block.

Detection is opt-in through RunOptions.KnownBlockKinds, the session's
membership test derived from the ComponentSet's declared kinds via
ComponentSet.KnownKinds plus kinds processed outside the component loop
("done" by the goal runner). The summary kind is exempt: the loop
extracts summary blocks before the check. A kindless block is
unavailable by construction — no component declares an empty kind.
Sessions that intentionally collect arbitrary block kinds (ping's
random validation kinds, next's dry-run change deliverable) leave the
predicate nil and keep the trust-collection behavior.

The correction shares the parse-error budget and reset semantics
(decideBlockCorrectionFeedback): one cumulative per-run budget for all
unprocessable blocks, reset only by a generation with none, so a
persistently mis-emitting model cannot loop corrections indefinitely.
Beyond the budget the unknown-kind blocks are not fed back; they stay
in Result.RemainingBlocks as before.
`

// unknownKindBlocks returns the collected blocks whose kind is not
// processable in this session according to knownKinds. Summary blocks
// are extracted before this check, so the summary kind never reaches the
// predicate; a kindless block (empty Kind) is unavailable by
// construction — no component declares an empty kind — and is reported.
// See TheoryOfUnknownBlockKinds.
func unknownKindBlocks(collected []blocks.Block, knownKinds func(kind string) bool) []blocks.Block {
	var unknown []blocks.Block
	for _, block := range collected {
		if !knownKinds(block.Kind) {
			unknown = append(unknown, block)
		}
	}
	return unknown
}

// decideBlockCorrectionFeedback decides whether to feed unprocessable
// blocks back to the model for self-correction: parse errors (malformed
// blocks), unknown-kind blocks (well-formed blocks whose kind the
// session cannot process), and naming errors (blocks whose session-tree
// parent/name parameters failed validation, discarding the whole
// batch). All categories wasted the attempt's output without taking
// effect, so they share one correction round and one cumulative
// per-run budget: the budget resets only when a generation produces
// none (returning a reset counter), so a model that persistently emits
// unprocessable blocks cannot restart the correction cycle indefinitely
// when other components keep triggering generations. When the budget is
// exhausted, no feedback is produced and the generation's parse errors
// are returned as uncorrected so the caller can record them in
// Result.ParseErrors; unknown-kind blocks beyond the budget stay in the
// remaining blocks. See TheoryOfLoops, TheoryOfUnknownBlockKinds, and
// TheoryOfSessionTree.
func decideBlockCorrectionFeedback(
	generationParseErrors []*blocks.BlockParseError,
	unknownKinds []blocks.Block,
	namingErrs []string,
	correctionCount int,
) (
	feedback []generators.Part,
	correctionCountOut int,
	skipOnAttemptStart bool,
	uncorrected []*blocks.BlockParseError,
) {
	if len(generationParseErrors) == 0 && len(unknownKinds) == 0 && len(namingErrs) == 0 {
		return nil, 0, false, nil
	}
	if correctionCount < maxParseErrorCorrections {
		correctionCount++
		var parts []generators.Part
		if len(generationParseErrors) > 0 {
			parts = append(parts, generators.Text(formatParseErrors(generationParseErrors, correctionCount, maxParseErrorCorrections)))
		}
		if len(unknownKinds) > 0 {
			parts = append(parts, generators.Text(formatUnknownKindFeedback(unknownKinds, correctionCount, maxParseErrorCorrections)))
		}
		if len(namingErrs) > 0 {
			parts = append(parts, generators.Text(formatNamingErrors(namingErrs, correctionCount, maxParseErrorCorrections)))
		}
		return parts, correctionCount, true, nil
	}
	return nil, correctionCount, false, generationParseErrors
}

// formatNamingErrors formats session-tree naming errors (duplicate node
// names, unknown parents, missing required parent/name attributes) as
// user content fed back to the model for correction. The whole block
// batch was discarded — no block node was written — so the model must
// re-emit every block of the batch with corrected parent/name header
// parameters. The message shares the parse-error style: it states the
// correction attempt against the shared budget and carries the resume
// directive. See TheoryOfSessionTree and TheoryOfUnknownBlockKinds.
func formatNamingErrors(namingErrs []string, attempt, maxAttempts int) string {
	var sb strings.Builder
	sb.WriteString("[System note: The blocks listed below could not be recorded in the session tree, and the WHOLE batch of block nodes from the previous response was discarded — no block was recorded, though blocks of other kinds may still have been processed. Fix the parent/name header parameters named below and re-emit every block of the batch. ")
	fmt.Fprintf(&sb, "This is correction attempt %d of %d; naming errors that persist after the final attempt are dropped without effect. ", attempt, maxAttempts)
	sb.WriteString("After the correction, CONTINUE the original task exactly where it stopped: the correction is not the completion of the task. Then end your response with a summary block.]\n\n")
	for _, namingErr := range namingErrs {
		sb.WriteString(namingErr)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// formatUnknownKindFeedback formats collected unknown-kind blocks as
// user content fed back to the model for correction. Unlike malformed
// blocks, an unavailable kind cannot be corrected by re-emitting it:
// the kind itself is the fault, so the model must not re-emit the
// blocks — it must use the kind's replacement behavior (the
// disabled-blocks notice states one when configured) or deliver the
// content as plain text. The message names each block's kind and
// boundary so the model can locate them in its own output, states the
// correction attempt number against the shared budget, and carries the
// resume directive: after the correction the model must continue the
// original task and end with a summary block. See
// TheoryOfUnknownBlockKinds and TheoryOfLoops.
func formatUnknownKindFeedback(unknown []blocks.Block, attempt, maxAttempts int) string {
	var sb strings.Builder
	sb.WriteString("[System note: The blocks listed below have kinds that are NOT available in this session. They were never processed — not executed, not applied, not answered — and no action they imply has happened. Do NOT re-emit them: the kind itself is the fault, so re-emitting repeats it. Use the replacement behavior the system prompt states for the kind (see the Disabled Block Kinds section), or deliver the content as plain text in the response body. ")
	fmt.Fprintf(&sb, "This is correction attempt %d of %d; blocks that stay unavailable after the final attempt are dropped without effect. ", attempt, maxAttempts)
	sb.WriteString("After the correction, CONTINUE the original task exactly where it stopped: the correction is not the completion of the task. Then end your response with a summary block.]\n\n")
	for _, block := range unknown {
		fmt.Fprintf(&sb, "unprocessed block: kind %q, boundary %q\n", block.Kind, block.Boundary)
	}
	sb.WriteString("\n")
	return sb.String()
}
