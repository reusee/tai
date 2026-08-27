package pipeline

import (
	"github.com/reusee/tai/generators"
)

// TheoryOfLoopEvents states the event-stream contract of the generation
// loop. See the constant body for the design.
const TheoryOfLoopEvents = `
Run is the loop's single event iterator: every notable occurrence during a
generation run — attempt lifecycle (start, completion, truncation, error),
retry decisions, handoffs, synthesized completion summaries, attempt finish
reasons, per-attempt token usage, periodic thought summaries,
component-triggered continuations, and idle-handler input — flows to the
consumer as one Event stream (iter.Seq2[Event, error]), unifying the
loop's architecture on the iterator pattern. Events are constructed and
yielded the moment their facts are known — an attempt's start event
precedes its work, a handoff-start event precedes the handoff request,
and a truncation event fires when truncation is detected, before the
handoff summary is requested — so a live consumer sees what is happening
as it happens; the terminal error, if any, arrives with the final yield's
error component and ends the sequence. The *Result is still filled
incrementally, so callers that only need the outcome drain the iterator
and read the result, while callers that want live signals (a TUI, an
observer) consume the events as they stream.

The attempt is the loop's bookkeeping unit: one pass through the phase
chain. Event.Attempt numbers attempts monotonically across the whole
run — retries and the attempts of component-triggered generations and
idle-handler inputs continue the sequence instead of restarting at 1 —
so a consumer sees one increasing attempt counter per session;
Event.AttemptInGeneration carries the attempt's 1-based position within
its generation's retry budget, pairing with MaxAttempts for the
truncated, retry, and handoff budget display. A generation completes
when an attempt finishes with a summary block and a normal finish
reason; its completion event carries the summary. Retries re-execute
the phase chain as a new attempt, up to the retry budget carried by
MaxAttempts.

Event.Loop attributes every event to its goal run: RunOptions.Loop
carries the 1-based goal loop number, and the loop's emit layer stamps
it onto every event it yields, so a consumer sees which goal loop
produced each attempt and usage. Non-goal runs carry Loop 0, and
displays omit the attribution for them. Goal progress joins the same
channel from the outside: RunGoal reports verdicts and failure notes as
EventGoal through GoalEventObserver, which a display front-end forks to
its event consumer (see TheoryOfGoalMode).

The TUI's Events tab renders this stream directly (cmd/tai taps the
iterator via withTUIOutputObserver), so every Events-tab line originates
from a Run event: finish reasons (EventFinish) and thought summaries
(EventThoughtSummary) are loop events, never side channels.

loopState owns the guarded yield: after the consumer stops, the iterator
contract forbids calling yield again, but the loop's bookkeeping — result
filling, recorder calls, EndSession — must still complete, so further
events are dropped instead of yielded. runGeneration executes inside Run's
iterator body and emits through the same guarded yield, so there is
exactly one event channel: the run's own iterator. Thought summaries are
produced synchronously inside phase execution on the loop's goroutine
(the ThoughtsSummarize state layer forwards through an emitter installed
by Module.Run), so their reentrant yield is safe. Functions that produce
values rather than occurrences — ProcessComponents, the Handoff option,
the attempt callbacks — keep their signatures: they are steps of the loop,
not streams, and the loop reports their outcomes as Events.

Events complement the InteractionRecorder rather than replacing it: the
recorder persists the full interaction transcript for analysis, while the
event stream is the in-band channel a live consumer observes during the
run.
`

// EventKind identifies the kind of a loop Event.
type EventKind string

const (
	EventAttemptStart EventKind = "attempt-start"
	// EventFinish reports the finish reason of one generation attempt
	// (Detail carries the reason string). Emitted immediately after the
	// attempt's finish reason is extracted, including attempts that
	// later fail or are truncated, so a live consumer observes every
	// request's completion signal as soon as it is known.
	EventFinish EventKind = "finish"
	// EventUsage reports an attempt's aggregated token usage; Detail
	// carries the outcome marker ("error") for attempts that end with
	// an error. Emitted once the attempt's usage is known, which on
	// retry paths is after the handoff's spend is injected, so the
	// recorded figure covers the whole attempt.
	EventUsage EventKind = "usage"
	// EventTruncated reports an attempt that ended without a completion
	// signal (no summary block or an abnormal finish reason) and will
	// be retried; Detail carries the reason. Emitted immediately when
	// truncation is detected, BEFORE the handoff request, so a live
	// consumer learns of the truncation without waiting for the handoff
	// generation. The truncated attempt's handoff summary is not
	// repeated here — EventHandoff already carries it.
	EventTruncated EventKind = "truncated"
	// EventRetry reports an attempt that failed with an error after
	// producing output and is retried; Err carries the error being
	// retried. Emitted immediately when the retry is decided.
	EventRetry EventKind = "retry"
	// EventHandoffStart reports that a handoff summary request is about
	// to be sent. Emitted immediately before every Handoff() call — on
	// both retry paths and the exhausted-budget synthesis path — so a
	// live consumer sees the handoff request in progress rather than
	// waiting for its result.
	EventHandoffStart EventKind = "handoff-start"
	// EventHandoff reports a handoff summary produced for a retry;
	// Handoff carries it and Summary its text. Emitted after the
	// handoff request returns, preceded by EventHandoffStart.
	EventHandoff EventKind = "handoff"
	// EventAttemptCompleted closes an attempt that completed normally;
	// Summaries carries the attempt's summary block bodies and Summary
	// their joined text.
	EventAttemptCompleted EventKind = "completed"
	// EventSynthesizedSummary reports that the retry budget was
	// exhausted and a completion summary was synthesized from the
	// attempt's output; Summary carries it.
	EventSynthesizedSummary EventKind = "synthesized-summary"
	// EventRunError closes the run with a terminal error: the run
	// stops and the event's error is also the iterator's terminal
	// error component.
	EventRunError EventKind = "run-error"
	// EventThoughtSummary reports a periodic thought summary produced
	// by the ThoughtsSummarize state layer during generation; Summary
	// carries the condensed text.
	EventThoughtSummary EventKind = "thought-summary"
	// EventComponentsTriggered reports that component output (or
	// parse-error feedback) scheduled the next generation; Detail
	// describes the trigger.
	EventComponentsTriggered EventKind = "components-triggered"
	// EventIdle reports that the idle handler provided user input for
	// the next generation.
	EventIdle EventKind = "idle"
)

// EventGoal reports one goal-run progress message: a verdict (achieved,
// not achieved, stopped, no-change completion) or a failure note.
// Detail carries the message text. Reported by the goal runner through
// GoalEventObserver; see TheoryOfGoalMode.
const EventGoal EventKind = "goal"

// Event is one notable occurrence during a generation loop run. Events
// are constructed and yielded the moment their facts are known; the
// terminal error, if any, arrives with the final yield's error
// component. See TheoryOfLoopEvents.
type Event struct {
	// Kind identifies the event.
	Kind EventKind
	// Loop is the 1-based goal loop number of the run that produced
	// the event, stamped by the loop's emit layer from RunOptions.Loop.
	// Zero for non-goal runs; displays omit the attribution for them.
	// See TheoryOfLoopEvents.
	Loop int
	// Attempt is the session-wide 1-based attempt number: one pass
	// through the phase chain, numbered monotonically across all
	// generations of the run — retries, component-triggered
	// generations, and idle-handler inputs continue the sequence — so
	// a consumer sees one increasing attempt counter per session.
	// Zero when not applicable.
	Attempt int
	// AttemptInGeneration is the attempt's 1-based position within its
	// generation's retry budget, pairing with MaxAttempts for the
	// truncated, retry, and handoff budget display ("attempt x/y").
	// Zero when not applicable.
	AttemptInGeneration int
	// MaxAttempts is the generation's retry budget, for attempt, retry,
	// truncation, and handoff events. Zero when retry is disabled.
	MaxAttempts int
	// Summary carries a summary text: the joined summary block bodies
	// for EventAttemptCompleted, the handoff summary for EventHandoff
	// and EventSynthesizedSummary, and the condensed thought summary
	// for EventThoughtSummary.
	Summary string
	// Summaries carries the attempt's summary block bodies for
	// EventAttemptCompleted.
	Summaries []string
	// Usage carries the attempt's aggregated token usage for
	// EventUsage.
	Usage generators.Usage
	// Handoff carries the handoff produced for a retry, for
	// EventHandoff and EventSynthesizedSummary.
	Handoff *Handoff
	// Err carries the error for EventRetry (the error being retried)
	// and EventRunError (the terminal error).
	Err error
	// Detail carries a human-readable description for less structured
	// events: the reason for EventRetry and EventTruncated, the finish
	// reason for EventFinish, the outcome marker ("error") for
	// EventUsage, and the message text for EventGoal.
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
	// The run's loop number attributes the event to its goal loop; the
	// events of a non-goal run keep Loop 0. See TheoryOfLoopEvents.
	ev.Loop = ls.opts.Loop
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
	ev.Loop = ls.opts.Loop
	ls.yield(ev, err)
}
