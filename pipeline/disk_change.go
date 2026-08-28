package pipeline

import "errors"

const TheoryOfDiskChangeHandoff = `
Disk-change handoff: when a change block apply or a flush fails because a
disk file no longer matches the content snapshot the loop's context was
built from (changes.DiskChangedError, see
changes.TheoryOfDiskChangeDetection), an attempt retry cannot repair the
divergence — the retry would compute changes against the same stale
snapshot. The generation loop therefore ends the run instead of retrying:
it condenses the interrupted output into a handoff when one is available,
wraps the failure in DiskChangeHandoffError, and terminates. In goal mode
the runner recognizes the error, ends the loop, and carries the handoff
content — the interrupted work's summary plus the disk-change notice —
into the next loop's feedback as GoalFeedback; the next loop re-reads the
current filesystem state and restarts the work from scratch. The
consecutive-error counter does not count a disk-change handoff: its cause
is external to the model. See TheoryOfGoalMode and TheoryOfLoops.
`

// DiskChangeHandoffError terminates a generation loop whose disk state
// diverged from the model's context snapshot: an attempt retry would
// compute changes against the same stale content, so the loop ends and
// the handoff — when one was produced — is carried into the next goal
// loop, which reloads the filesystem. Callers detect it with errors.As.
// See TheoryOfDiskChangeHandoff.
type DiskChangeHandoffError struct {
	// Err is the underlying failure, e.g. *changes.DiskChangedError.
	Err error
	// Handoff condenses the interrupted output; nil when the output was
	// too short to summarize or no summarizer was configured.
	Handoff *Handoff
}

func (e *DiskChangeHandoffError) Error() string {
	return e.Err.Error()
}

func (e *DiskChangeHandoffError) Unwrap() error {
	return e.Err
}

// asDiskChangeHandoff reports whether err carries a disk-change handoff.
func asDiskChangeHandoff(err error) (*DiskChangeHandoffError, bool) {
	var handoff *DiskChangeHandoffError
	if err == nil {
		return nil, false
	}
	if errors.As(err, &handoff) {
		return handoff, true
	}
	return nil, false
}
