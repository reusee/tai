package pipeline

import (
	"github.com/reusee/tai/tree"
)

const TheoryOfLoopEvents = `
Run is the loop's single tree iterator: every notable occurrence during
a generation run — attempt lifecycle (start, completion, truncation,
error), the actual request parameters, retry decisions, handoffs,
synthesized completion summaries, attempt finish reasons, per-attempt
token usage, periodic thought summaries, component-triggered
continuations, and idle-handler input — is recorded as one event node
(an event-subtype type in Category event, program author) in the
session tree, and then the FULL tree is yielded to the consumer
(iter.Seq2[*tree.Tree, error]). There is no separate event stream: the
events mechanism is fully merged into the tree, so the display
front-end renders — and projects — the same tree the pipeline writes,
never a separately maintained copy. Occurrences are recorded and
yielded the moment their facts are known — an attempt's start node
precedes its work, a handoff-start node precedes the handoff request,
and a truncation node fires when truncation is detected, before the
handoff summary is requested — so a live consumer sees what is
happening as it happens; the terminal error, if any, arrives with the
final yield's error component and ends the sequence. The *Result is
still filled incrementally, so callers that only need the outcome drain
the iterator and read the result, while callers that want live signals
(a TUI, an observer) consume the trees as they stream.

Event nodes hang under the current attempt node — one attempt
structure node (tree.TypeAttempt) per attempt, written under the
session parent (the tree root for a fresh run, the goal run's loop-N
node for a continued one) when the attempt opens — so the tree
position attributes every occurrence to its attempt and, through it,
to its goal loop; no attempt or loop number is stamped onto the node.
The attempt-start event node is the structure node's first child:
writeEventNode opens the structure node when it records the attempt's
start. The node type IS the event kind — one of the event subtypes
(attempt-start, request, finish, usage, truncated, retry,
handoff-start, handoff, completed, synthesized-summary,
thought-summary, continue, idle, run-error), and the goal runner
writes goal verdicts as tree.TypeGoal nodes — while the node name
carries the subtype as a prefix, made unique by AutoName, so typed
event nodes keep their historical names. The node content is the
human-readable description; multi-line content (handoff and completion
summaries) collapses by default in the display front-end's Tree tab.
Event nodes are program bookkeeping: every model-facing outline
excludes them by category (treeOutlinePart, handoffOutlinePart), so
the model never sees the loop's own bookkeeping.

The attempt is the loop's bookkeeping unit: one pass through the
phase chain, and one attempt structure node in the tree — the
attempt's events, response, summaries, blocks, errors, feedback, and
idle input hang under it, while the session's system and initial user
nodes stay the attempt nodes' siblings. Attempt numbering is
session-wide within one run — retries and the attempts of
component-triggered generations and idle-handler inputs continue the
sequence instead of restarting at 1 — and the attempt number appears
in the event node contents. A generation completes when an attempt
finishes with a summary block and a normal finish reason; the
completed node carries the summary. Retries re-execute the phase
chain as a new attempt, up to the retry budget.

The request node precedes each attempt's request: its content
describes the actual generation parameters — the resolved spec path,
the model identity, and the effective temperature, reasoning effort,
and token limits — resolved from the generator spec with the
temperature and effort flag overrides applied (the flags are dscope
provided and captured by the Module.Run provider, mirroring the
generators' flag-over-spec precedence). The node is the loop-level
view: retries internal to the generator's Retrier are separate API
calls not visible here, so one loop attempt may cover several
requests.

Thought summaries join the same tree: the ThoughtsSummarize state
layer forwards through an emitter installed by Module.Run, which
writes a thought-summary event node and yields the tree. Goal
progress joins from the outside: RunGoal records verdicts and failure
notes as tree.TypeGoal event nodes under the tree root and forwards
the tree through GoalTreeObserver (see TheoryOfGoalMode).

loopState owns the guarded yield: after the consumer stops, the
iterator contract forbids calling yield again, but the loop's
bookkeeping — result filling, recorder calls, EndSession — must still
complete, so event nodes are still written while further yields are
dropped. runGeneration executes inside Run's iterator body and emits
through the same guarded yield, so there is exactly one channel: the
run's own iterator. Thought summaries are produced synchronously
inside phase execution on the loop's goroutine, so their reentrant
yield is safe. Functions that produce values rather than occurrences
— ProcessComponents, the Handoff option, the attempt callbacks — keep
their signatures: they are steps of the loop, not streams, and the
loop records their outcomes as event nodes.

Event nodes complement the InteractionRecorder rather than replacing
it: the recorder persists the full interaction transcript for
analysis, while the tree is the in-band channel a live consumer
observes during the run.
`

// writeEventNode records one loop occurrence as an event node of the
// given event subtype in the session tree and yields the full tree to
// the consumer. The node name carries the subtype as a prefix, made
// unique by AutoName, so typed event nodes keep their historical
// names. The node hangs under the current attempt node: the
// attempt's occurrences are its record. An attempt-start event opens
// the attempt's structure node first and writes the initial user
// input under it, so the event is the attempt node's first child and
// the input joins the attempt that consumes it. The node is written
// even after the consumer has stopped — the tree is the run's record
// — while the yield is dropped. See TheoryOfLoopEvents.
func (ls *loopState) writeEventNode(typ tree.Type, content string) {
	if ls.sessionTree == nil {
		return
	}
	if typ == tree.TypeAttemptStart {
		ls.writeAttemptNode()
	}
	next, _, err := ls.sessionTree.WriteAuto(ls.attemptParent(), string(typ), typ, tree.AuthorProgram, content)
	if err != nil {
		return
	}
	ls.sessionTree = next
	if typ == tree.TypeAttemptStart {
		// The initial user input joins the first attempt node, written
		// right after the attempt opens and before its request. See
		// TheoryOfSessionTree.
		ls.writeInitialUserNode()
	}
	ls.emitTree()
}

// emitTree yields the run's current session tree. After the consumer
// stops, the iterator contract forbids calling yield again, but the
// loop's bookkeeping — result filling, recorder calls, EndSession —
// must still complete, so further yields are dropped instead of sent.
// Returns false once the consumer has stopped. See TheoryOfLoopEvents.
func (ls *loopState) emitTree() bool {
	if ls.stopped || ls.sessionTree == nil {
		return !ls.stopped
	}
	if !ls.yield(ls.sessionTree, nil) {
		ls.stopped = true
	}
	return !ls.stopped
}

// emitTerminalTree yields the final (tree, error) pair that ends the
// run and marks the consumer as stopped, so no further yield is
// attempted. Like emitTree it is a no-op after the consumer has
// already stopped. See TheoryOfLoopEvents.
func (ls *loopState) emitTerminalTree(err error) {
	if ls.stopped {
		return
	}
	ls.stopped = true
	ls.yield(ls.sessionTree, err)
}
