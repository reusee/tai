package pipeline

import (
	"errors"
	"fmt"

	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/tree"
)

const TheoryOfFirstAttemptAbandonment = `
First-attempt abandonment: a goal loop's first attempt decides the
loop. When the first attempt needs recovery — a truncated or abnormal
response without the summary block, an error after content was
produced, or well-formed blocks that need a correction round — the run
abandons it instead of recovering it: no handoff request condenses
the interrupted output, no retry attempt re-sends the same context, and
no correction round follows. The run ends; the goal runner folds the
outcome into the next loop's feedback like any other loop outcome, and
the next loop retries from scratch with a freshly assembled context and
the current filesystem state.

The reason is the meaning of the failure. A first attempt that needs a
handoff or a correction round shows that its output did not meet the
format rules, not that its thinking ran too long. The loop's start is
clean — a fresh scope, a fresh conversation, the current filesystem
state — so a failure there says the output format is the problem, while
the recovery machinery exists to preserve the thinking of a conversation
that is already in progress. Retrying on the same conversation repeats
the conditions that produced the non-conforming output; the next loop's
clean start is the better retry.

Only the first attempt is abandoned. A loop that already produced a
conforming response keeps the ordinary recovery machinery for its later
attempts: the conversation demonstrably works, so the handoff and retry
paths apply there.

The abandonment reports the cause. A failure that carries its own
terminal type keeps it, with a nil handoff: a disk change keeps
DiskChangeHandoffError, an exceeded context window keeps
ContextExceededError. The runner therefore applies that cause's own
feedback and counting rule to the next loop — the reload notice and the
consecutive-error exemption for a disk change, the smaller-context
notice and the counted failure for an exceeded window. Every other
first-attempt failure ends the loop with FirstAttemptAbandonedError,
which the runner treats as an ordinary loop error and counts toward the
consecutive-error bound. The bound therefore still caps a run that keeps
meeting the same first-attempt fault.

An attempt that never reached the success path records its produced
content, its usage, and its attempt statistics at abandonment, so the
session tree and the attempt table keep it. Its in-memory changes are
never flushed: nothing of a truncated or failed attempt reaches the
disk, and the next loop re-applies the intended changes against the
fresh snapshot. An attempt that already reached the success path — one
whose blocks were well formed but partly unprocessable, so it needs a
correction round — records nothing more: its content, usage, and
statistics are already in the record, and its applied changes are
already on disk for the next loop to verify.

RunOptions.AbortOnFirstAttemptFailure selects the policy. Module.Run
enables it for every run whose scope carries a positive GoalLoop, so
every goal loop abandons its first failed attempt, while an attended
session that is not a goal loop keeps the recovery paths described by
TheoryOfLoops and TheoryOfHandoff.
`

// FirstAttemptAbandonedError reports that a run abandoned its first
// attempt instead of recovering it, so the caller can retry the whole
// task from a fresh context. See TheoryOfFirstAttemptAbandonment.
type FirstAttemptAbandonedError struct {
	// Reason states what the first attempt did wrong, e.g. that the
	// response ended without the summary block.
	Reason string
	// Err is the underlying failure; nil when the attempt itself
	// completed but its output needed correction.
	Err error
}

func (e *FirstAttemptAbandonedError) Error() string {
	if e.Err == nil {
		return "first attempt abandoned: " + e.Reason
	}
	return "first attempt abandoned: " + e.Reason + ": " + e.Err.Error()
}

// Unwrap exposes the underlying failure to errors.Is and errors.As.
func (e *FirstAttemptAbandonedError) Unwrap() error {
	return e.Err
}

// abandonFirstAttempt ends the run on the first attempt's failure
// instead of recovering it: no handoff request, no retry attempt, and
// no correction round. A failure that carries its own terminal type
// keeps it, with a nil handoff, so the runner applies that cause's own
// feedback and counting rule to the next loop: a disk change keeps
// DiskChangeHandoffError, an exceeded context window keeps
// ContextExceededError, and every other failure ends the loop with
// FirstAttemptAbandonedError. An attempt that never reached the success
// path records its produced content, its usage, and its attempt
// statistics here. An attempt that already reached it — its response
// node is written, so its content, usage, and statistics are already in
// the record and its applied changes are already on disk — records only
// the abandonment event. The in-memory changes of an uncommitted
// attempt are never flushed, so nothing of it reaches the disk. See
// TheoryOfFirstAttemptAbandonment.
func (ls *loopState) abandonFirstAttempt(
	phaseState generators.State,
	attemptBase int,
	reason string,
	cause error,
) error {
	// The attempt's own model node marks the success path: the attempt
	// wrote it with its response, summaries, and blocks, and only a
	// failed attempt reaches this method without one. See
	// TheoryOfSessionTree.
	committed := false
	if ls.sessionTree != nil {
		if attempt, ok := ls.sessionTree.Node(ls.currentAttempt); ok {
			for _, child := range attempt.Children() {
				if child.Type == tree.TypeModel {
					committed = true
					break
				}
			}
		}
	}
	if !committed {
		ls.recordFailedAttemptTree(phaseState, attemptBase)
		ls.recordAttemptUsage(phaseState, attemptBase, "abandoned")
		if ls.opts.OnAttemptTruncated != nil {
			if hookErr := ls.opts.OnAttemptTruncated(phaseState, ls.state, "[Abandoned: "+reason+"]"); hookErr != nil {
				cause = errors.Join(cause, hookErr)
			}
		}
	}
	ls.writeEventNode("abandoned", fmt.Sprintf(
		"attempt %d abandoned by the first-attempt policy: %s", ls.attempt, reason))
	// The cause's own terminal type carries its own recovery notice and
	// counting rule, so the runner keeps both. See
	// TheoryOfDiskChangeHandoff and TheoryOfContextExceededHandoff.
	if cause != nil {
		var diskChanged *changes.DiskChangedError
		if errors.As(cause, &diskChanged) {
			return &DiskChangeHandoffError{Err: cause}
		}
		if generators.IsContextExceeded(cause) {
			return &ContextExceededError{Err: cause}
		}
	}
	return &FirstAttemptAbandonedError{Reason: reason, Err: cause}
}

// abortsOnFirstAttempt reports whether the first-attempt policy applies
// to the current attempt: the policy is enabled and this is the run's
// first attempt. A later attempt belongs to a loop that already
// produced conforming output, so it keeps the ordinary recovery paths.
// See TheoryOfFirstAttemptAbandonment.
func (ls *loopState) abortsOnFirstAttempt() bool {
	return ls.opts.AbortOnFirstAttemptFailure && ls.attempt == 1
}
