package pipeline

import (
	"errors"
	"fmt"
	"strings"

	"github.com/reusee/tai/generators"
)

const TheoryOfContextExceededHandoff = `
Context-exceeded handoff: when a generation attempt fails because the
request exceeded the model's context window (generators.IsContextExceeded),
an attempt retry cannot repair the failure — the same messages exceed the
window again, and a retried generation appends error feedback and a
handoff that grow the input further. The generation loop therefore ends
the run instead of retrying: it condenses the interrupted output into a
handoff when one is available, wraps the failure in ContextExceededError,
and terminates. In goal mode the runner recognizes the error, ends the
loop, and carries the handoff content — the interrupted work's summary
plus the fresh-context notice — into the next loop's feedback as
GoalFeedback; the next loop's freshly assembled context is naturally
smaller, so the run continues the interrupted work there. Unlike a
disk-change handoff, the over-limit context is accumulated by the loop
itself, so the failure counts toward the consecutive-error bound: three
identical failures stop the run with a diagnostic. See
TheoryOfDiskChangeHandoff, TheoryOfGoalMode and TheoryOfLoops.
`

// ContextExceededError terminates a generation loop whose request
// exceeded the model's context window: an attempt retry would exceed it
// again with a larger input, so the loop ends and the handoff — when one
// was produced — is carried into the next goal loop, whose fresh scope
// rebuilds a smaller context. Callers detect it with errors.As. See
// TheoryOfContextExceededHandoff.
type ContextExceededError struct {
	// Err is the underlying API error.
	Err error
	// Handoff condenses the interrupted output; nil when the output was
	// too short to summarize or no summarizer was configured.
	Handoff *Handoff
}

func (e *ContextExceededError) Error() string {
	return e.Err.Error()
}

func (e *ContextExceededError) Unwrap() error {
	return e.Err
}

// asContextExceeded reports whether err carries a context-exceeded
// handoff.
func asContextExceeded(err error) (*ContextExceededError, bool) {
	var exceeded *ContextExceededError
	if err == nil {
		return nil, false
	}
	if errors.As(err, &exceeded) {
		return exceeded, true
	}
	return nil, false
}

// endOnContextExceeded terminates the run on a context-exceeded failure:
// it condenses the interrupted output into a handoff when one is
// available, records the failed attempt, and returns the terminal error
// the goal runner forwards to the next loop. See
// TheoryOfContextExceededHandoff.
func (ls *loopState) endOnContextExceeded(err error, phaseState generators.State, attemptBase int) *ContextExceededError {
	return &ContextExceededError{Err: err, Handoff: ls.endWithHandoff(err, phaseState, attemptBase)}
}

// applyContextExceededHandoff folds a loop that ended on a
// context-exceeded handoff into the runner state: the handoff content —
// the interrupted work's summary plus the fresh-context notice — becomes
// the next loop's feedback, and the next loop's freshly assembled context
// is naturally smaller, so the run continues the interrupted work. Unlike
// a disk change, the over-limit context is accumulated by the loop
// itself, so the failure counts toward the consecutive-error bound: three
// identical failures stop the run with a diagnostic. A pending done
// declaration is overturned. See TheoryOfGoalMode and
// TheoryOfContextExceededHandoff.
func (s *goalLoopState) applyContextExceededHandoff(
	loopsRun int,
	err *ContextExceededError,
	reporter goalReporter,
) bool {
	reporter.message(fmt.Sprintf(
		"\n[Goal Loop %d Ended Early: %v. The next loop starts with a freshly assembled, smaller context and continues the work.]\n",
		loopsRun, err.Err))

	s.pendingDoneVerification = false

	errMsg := err.Error()
	if errMsg == s.lastErrMsg {
		s.consecutiveErrors++
	} else {
		s.consecutiveErrors = 1
		s.lastErrMsg = errMsg
	}
	if s.consecutiveErrors >= maxConsecutiveGoalErrors {
		reporter.failure(fmt.Sprintf(
			"\n[Goal Stopped: the same error occurred %d consecutive times]\n%s\n",
			maxConsecutiveGoalErrors, errMsg))
		s.stopRequested = true
		return true
	}

	feedback := "[System note: The previous goal loop ended early: " + err.Err.Error() + ". The model context exceeded the window, so no attempt retry could repair it. This loop starts with a freshly assembled, smaller context: re-read the relevant files, continue the interrupted work, and partition the work across rounds so the context stays within the window."
	if err.Handoff != nil && strings.TrimSpace(err.Handoff.Prompt) != "" {
		feedback += "\n\nHandoff summary of the interrupted work:\n" + err.Handoff.Prompt
	}
	feedback += "]"
	s.feedback = GoalFeedback(feedback)
	return false
}
