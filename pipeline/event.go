package pipeline

import (
	"github.com/reusee/tai/generators"
)

// TheoryOfLoopEvents states the event-stream contract of the generation
// loop. See the constant body for the design.
const TheoryOfLoopEvents = `
Run is the loop's single event iterator: every notable occurrence during a
generation run — round lifecycle (start, success, truncation, error),
retry decisions and handoffs, synthesized completion summaries, per-round
token usage, component-triggered continuations, and idle-handler input —
flows to the consumer as one Event stream (iter.Seq2[Event, error]),
unifying the loop's architecture on the iterator pattern. Events are
yielded as they occur; the terminal error, if any, arrives with the final
yield's error component and ends the sequence. The *Result is still
filled incrementally, so callers that only need the outcome drain the
iterator and read the result, while callers that want live signals (a
TUI, an observer) consume the events as they stream.

loopState owns the guarded yield: after the consumer stops, the iterator
contract forbids calling yield again, but the loop's bookkeeping — result
filling, recorder calls, EndSession — must still complete, so further
events are dropped instead of yielded. runRound executes inside Run's
iterator body and emits through the same guarded yield, so there is
exactly one event channel: the run's own iterator. Functions that produce
values rather than occurrences — ProcessComponents, the Handoff option,
the round callbacks — keep their signatures: they are steps of the loop,
not streams, and the loop reports their outcomes as Events.

Events complement the InteractionRecorder rather than replacing it: the
recorder persists the full interaction transcript for analysis, while the
event stream is the in-band channel a live consumer observes during the
run.
`

// EventKind identifies the kind of a loop Event.
type EventKind string

const (
	// EventRoundStart opens a generation round.
	EventRoundStart EventKind = "round-start"
	// EventRoundSuccess closes a round that completed normally;
	// Summaries carries the round's summary block bodies and Summary
	// their joined text.
	EventRoundSuccess EventKind = "round-success"
	// EventRoundTruncated reports an attempt that ended without a
	// completion signal (no summary block or an abnormal finish
	// reason) and is retried. Summary carries the handoff summary of
	// the truncated attempt, Detail the reason.
	EventRoundTruncated EventKind = "round-truncated"
	// EventRetry reports an attempt that failed with an error after
	// producing output and is retried; Err carries the error being
	// retried.
	EventRetry EventKind = "retry"
	// EventRoundError closes a round with a terminal error: the run
	// stops and the event's error is also the iterator's terminal
	// error component.
	EventRoundError EventKind = "round-error"
	// EventHandoff reports a handoff summary produced for a retry;
	// Handoff carries it and Summary its text.
	EventHandoff EventKind = "handoff"
	// EventSynthesizedSummary reports that the retry budget was
	// exhausted and a completion summary was synthesized from the
	// round's output; Summary carries it.
	EventSynthesizedSummary EventKind = "synthesized-summary"
	// EventUsage reports a round's aggregated token usage.
	EventUsage EventKind = "usage"
	// EventComponentsTriggered reports that component output (or
	// parse-error feedback) scheduled the next round; Detail
	// describes the trigger.
	EventComponentsTriggered EventKind = "components-triggered"
	// EventIdle reports that the idle handler provided user input for
	// the next round.
	EventIdle EventKind = "idle"
)

// Event is one notable occurrence during a generation loop run. Events
// are yielded by Run as they occur; the terminal error, if any, arrives
// with the final yield's error component. See TheoryOfLoopEvents.
type Event struct {
	// Kind identifies the event.
	Kind EventKind
	// Round is the 1-based round number the event belongs to. Retries
	// within a round share the round number.
	Round int
	// Attempt is the 1-based attempt (retry) number within the round,
	// for retry, truncation, and handoff events. Zero when not
	// applicable.
	Attempt int
	// MaxAttempts is the round's retry budget, for retry, truncation,
	// and handoff events. Zero when not applicable.
	MaxAttempts int
	// Summary carries a summary text: the joined summary block bodies
	// for EventRoundSuccess, the handoff summary for EventHandoff,
	// EventRoundTruncated, and EventSynthesizedSummary.
	Summary string
	// Summaries carries the round's summary block bodies for
	// EventRoundSuccess.
	Summaries []string
	// Usage carries the round's aggregated token usage for EventUsage.
	Usage generators.Usage
	// Handoff carries the handoff produced for a retry, for
	// EventHandoff and EventSynthesizedSummary.
	Handoff *Handoff
	// Err carries the error for EventRetry (the error being retried)
	// and EventRoundError (the terminal error).
	Err error
	// Detail carries a human-readable description for less structured
	// events (EventRetry, EventRoundTruncated,
	// EventComponentsTriggered).
	Detail string
}

// emitEvent yields one event to the run's consumer. After the consumer
// stops, the iterator contract forbids calling yield again, but the
// loop's bookkeeping — result filling, recorder calls, EndSession — must
// still complete, so further events are dropped instead of yielded.
// Returns false once the consumer has stopped.
func (ls *loopState) emitEvent(ev Event) bool {
	if ls.stopped {
		return false
	}
	if !ls.yield(ev, nil) {
		ls.stopped = true
	}
	return !ls.stopped
}

// emitTerminal yields the final (event, error) pair that ends the run
// and marks the consumer as stopped, so no further yield is attempted.
// Like emitEvent it is a no-op after the consumer has already stopped.
func (ls *loopState) emitTerminal(ev Event, err error) {
	if ls.stopped {
		return
	}
	ls.stopped = true
	ls.yield(ev, err)
}
