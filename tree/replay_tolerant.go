package tree

import (
	"fmt"
	"strings"
)

// TheoryOfReplayTolerance states why the reconstruction of a recorded
// operation stream keeps what applies instead of failing on the first
// operation it cannot apply. See ReplayTolerant.
const TheoryOfReplayTolerance = `
Replay tolerance:
- A recorded operation stream is evidence, not a live session. It can hold
  operations the live tree would never have produced together — a duplicate
  name from a stream that diverged, a write whose parent never entered the
  stream — and one such operation must not hide the whole session from a
  reader.
- The reconstruction therefore keeps what applies. Every operation that
  applies is applied in stream order, the nodes keep the names, parents, and
  insert times the stream recorded, and the skipped operations travel beside
  the tree as the loader's diagnosis. Skipping never renames or rewrites
  another node, so a jump from a query or a search still lands on the node it
  names.
- Strictness stays available: Replay reports the first operation it cannot
  apply, for a caller that needs the reconstruction to equal the live
  sequence exactly.
- The problems are the loader's, not the session's: they are returned beside
  the tree and never written into it as nodes.
`

// ReplayProblem is one recorded operation that ReplayTolerant could not
// apply: its 0-based index in the stream, the operation itself, and the
// reason. See TheoryOfReplayTolerance.
type ReplayProblem struct {
	Index int
	Op    Op
	Err   error
}

// Error renders the problem as one diagnostic line: the operation's
// position, kind, and name, then the reason. The content stays out, because a
// recorded node can hold a whole file. See TheoryOfReplayTolerance.
func (p ReplayProblem) Error() string {
	return fmt.Sprintf("operation %d (%s %q): %v", p.Index, p.Op.Kind, p.Op.Name, p.Err)
}

// ReplayProblems is the set of operations a tolerant replay skipped. It
// renders compactly — a stream that diverged early can skip many operations —
// and it is an error, so a loader returns it beside the tree it rebuilt.
// See TheoryOfReplayTolerance.
type ReplayProblems []ReplayProblem

// Error states how many operations were skipped and names the first few
// reasons. The tree itself is not part of the message: the tree is what the
// caller renders. See TheoryOfReplayTolerance.
func (p ReplayProblems) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "replay skipped %d operation(s); the tree carries every operation that applied", len(p))
	const shown = 3
	for i, problem := range p {
		if i == shown {
			fmt.Fprintf(&b, "; and %d more", len(p)-shown)
			break
		}
		fmt.Fprintf(&b, "; %s", problem)
	}
	return b.String()
}

// ReplayTolerant rebuilds a tree from an operation stream the way Replay
// does, but keeps going when an operation cannot be applied: the
// reconstruction carries every operation that applied, and the operations it
// could not apply are returned beside it. A recorded stream is evidence, not
// a live session — one duplicate name in a long record must not hide the
// whole session from a reader. The reconstruction applies in place, so its
// cost stays linear in the stream. See TheoryOfReplayTolerance.
func ReplayTolerant(ops []Op) (*Tree, ReplayProblems) {
	tr := New()
	b := &replayBuilder{root: tr.root, byName: tr.byName}
	var problems ReplayProblems
	for i, op := range ops {
		if err := b.apply(op); err != nil {
			problems = append(problems, ReplayProblem{Index: i, Op: op, Err: err})
		}
	}
	return tr, problems
}
