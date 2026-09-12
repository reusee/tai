package pipeline

import (
	"github.com/reusee/tai/tree"
)

const TheoryOfLoopEvents = `
Run is the loop's single tree iterator: every notable occurrence during
a generation run — attempt lifecycle, the generator spec of each
attempt, retry decisions, handoffs, synthesized completion summaries,
attempt finish reasons, per-attempt token usage, periodic thought
summaries, generator-level API errors, component-triggered
continuations, idle-handler input, and the context-assembly
diagnostics replayed at startup — is recorded as a node in the session
tree, and then the FULL tree is yielded to the consumer
(iter.Seq2[*tree.Tree, error]). The finish reason and the reasoning
thoughts are the model's output, so they join the session's messages
as message-category nodes of types finish and thoughts; the rest are
the loop's own bookkeeping, recorded as event nodes (event-subtype
types, program author). There is no separate event stream: the
events mechanism is fully merged into the tree, so the display
front-end renders — and projects — the same tree the pipeline writes,
never a separately maintained copy. Occurrences are recorded and
yielded the moment their facts are known — an attempt node precedes
its work, a handoff-start node precedes the handoff request, and a
truncation node fires when truncation is detected, before the handoff
summary is requested — so a live consumer sees what is happening as it
happens; the terminal error, if any, arrives with the final yield's
error component and ends the sequence. The *Result is still filled
incrementally, so callers that only need the outcome drain the
iterator and read the result, while callers that want live signals
(a TUI, an observer) consume the trees as they stream.

Event nodes hang under the current attempt node — one attempt
structure node (tree.TypeAttempt) per attempt, written under the
session parent (the tree root for a fresh run, the goal run's loop-N
node for a continued one) when the attempt opens, with the user
prompts the attempt consumes written right after it — so the tree
position attributes every occurrence to its attempt and, through it,
to its goal loop; no attempt or loop number is stamped onto the node.
The loop's event nodes carry an event-subtype type — one of the event
subtypes (generator, usage, truncated, retry, handoff-start, handoff,
synthesized-summary, thought-summary, api_error, continue, idle,
run-error, context) — and the goal runner writes goal verdicts as
tree.TypeGoal structure nodes, while the node name carries the subtype
as a prefix, made unique by AutoName, so typed event nodes carry
their kind in their names. The node content is the human-readable
description; multi-line content (handoff summaries) collapses by
default in the display front-end's Tree tab. Change blocks and their
results are block nodes, not event nodes; api_error is the loop's own
record of the generator's API-level failures, and the finish and
thoughts nodes are the loop's record of the attempt's model output.
Event nodes are program bookkeeping: every model-facing outline
excludes them by category (treeOutlinePart, handoffOutlinePart), so
the model never sees the loop's own bookkeeping.

The attempt is the loop's bookkeeping unit: one pass through the
phase chain, and one attempt structure node in the tree — the
attempt's events, response, summaries, blocks, and errors hang under
it, together with the user prompts the attempt consumes: the initial
input on the first attempt, the previous round's feedback and idle
input queued since the previous attempt opened, and the chat phase's
input consumed inside the attempt's own phase chain. The session's
system node stays the attempt nodes' sibling. Attempt numbering is
session-wide within one run — retries and the attempts of
component-triggered generations and idle-handler inputs continue the
sequence instead of restarting at 1 — and the attempt number appears
in the attempt node's content and the event node contents. A
generation completes when an attempt finishes with a summary block
and a normal finish reason. Retries re-execute the phase chain as a
new attempt, up to the retry budget.

The generator node precedes each attempt's request: its content is
the generator spec the attempt runs on — the resolved spec path, the
model identity, and the effective temperature, reasoning effort, and
token limits — resolved from the spec with the temperature and effort
flag overrides applied (the flags are dscope provided and captured by
the Module.Run provider, mirroring the generators' flag-over-spec
precedence). The node is the loop-level view: retries internal to the
generator's Retrier are separate API calls not visible here, so one
loop attempt may cover several requests. Generator-level events the
generator writes through the scope's generators.EventRecorder — API
errors — are buffered in the scope's generators.EventSink and drained
by the loop after each attempt's phase chain and at the run's end,
becoming event nodes of the same type under the attempt that served
the request. The attempt's reasoning thoughts are recorded as one
thoughts message node alongside the attempt's model node, so the
trace joins the model output in the record. A failed attempt records
the same two nodes before its retry feedback or handoff request: one
thoughts message node for the reasoning trace and one model node for
the body text, so the tree carries every attempt's already-generated
content, not only the successful ones.

Thought summaries join the same tree: the ThoughtsSummarize state
layer forwards through an emitter installed by Module.Run, which
writes a thought-summary event node and yields the tree. Goal
progress joins from the outside: RunGoal records verdicts and failure
notes as tree.TypeGoal structure nodes under the tree root and
forwards the tree through GoalTreeObserver (see TheoryOfGoalMode).
Context-assembly diagnostics join at startup: Module.Run drains the
per-scope gotools.TreeEventSink before the first attempt and replays
each buffered record as a context event node under the session root,
because context assembly runs before the tree opens (see
gotools.TheoryOfTokenComposition).

loopState owns the guarded yield: after the consumer stops, the
iterator contract forbids calling yield again, but the loop's
bookkeeping — result filling, session bookkeeping, EndSession — must
still complete, so event nodes are still written while further yields
are dropped. runGeneration executes inside Run's iterator body and
emits through the same guarded yield, so there is exactly one channel:
the run's own iterator. Thought summaries are produced synchronously
inside phase execution on the loop's goroutine, so their reentrant
yield is safe. Functions that produce values rather than occurrences
— ProcessComponents, the Handoff option, the attempt callbacks — keep
their signatures: they are steps of the loop, not streams, and the
loop records their outcomes as event nodes.

The recorder persists the session's tree operation stream
(records.TheoryOfInteractionRecording), not a parallel event stream:
a fresh run opens the session through the resolved recorder and
attaches its sink to the session tree, so every node write — event
nodes included — is one recorded operation, and the session ends with
the run's terminal error. A continued run — a goal loop — leaves
session ownership to the runner, which attached the sink and opened
the session before the loop. The tree is therefore both the record and
the in-band channel a live consumer observes during the run.
`

// writeEventNode records one loop occurrence as a session-tree node of
// the given type and yields the full tree to the consumer. A type that
// carries no prefix names an event subtype: the node type is completed to
// EventType(name), so the loop's call sites pass the bare subtype names
// ("retry", "usage") and the full types where the occurrence is not an
// event (message::finish, message::thoughts). The node name carries the
// type's name segment as a prefix, made unique by AutoName, so typed event
// nodes carry their kind in their names. The node hangs under the current
// attempt node: the attempt's occurrences are its record. The node is
// written even after the consumer has stopped — the tree is the run's
// record — while the yield is dropped. See TheoryOfLoopEvents.
func (ls *loopState) writeEventNode(typ tree.Type, content string) {
	if ls.sessionTree == nil {
		return
	}
	if typ.Prefix() == string(typ) {
		typ = tree.EventType(string(typ))
	}
	next, _, err := ls.sessionTree.WriteAuto(ls.attemptParent(), typ.Name(), typ, tree.AuthorProgram, content)
	if err != nil {
		return
	}
	ls.sessionTree = next
	ls.emitTree()
}

// emitTree yields the run's current session tree. After the consumer
// stops, the iterator contract forbids calling yield again, but the
// loop's bookkeeping — result filling, recorder calls, EndSession —
// must still complete, so further yields are dropped instead of sent.
// A nil yield — a loop state with no consumer, as built directly in
// tests — is treated as a stopped consumer: the yields are dropped.
// Returns false once the consumer has stopped. See TheoryOfLoopEvents.
func (ls *loopState) emitTree() bool {
	if ls.stopped || ls.sessionTree == nil {
		return !ls.stopped
	}
	if ls.yield == nil {
		ls.stopped = true
		return false
	}
	if !ls.yield(ls.sessionTree, nil) {
		ls.stopped = true
	}
	return !ls.stopped
}

// emitTerminalTree yields the final (tree, error) pair that ends the
// run and marks the consumer as stopped, so no further yield is
// attempted. Like emitTree it is a no-op after the consumer has
// already stopped, and a nil yield — no consumer — drops the yield.
// See TheoryOfLoopEvents.
func (ls *loopState) emitTerminalTree(err error) {
	if ls.stopped {
		return
	}
	ls.stopped = true
	if ls.yield != nil {
		ls.yield(ls.sessionTree, err)
	}
}
