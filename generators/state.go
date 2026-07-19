package generators

import "iter"

const TheoryOfStateImmutability = `
State implementations must be strictly immutable: every modification operation
(AppendContent, Flush) returns a new State instance rather than mutating the
receiver in place. This invariant is the foundation for snapshot, rollback, and
retry semantics. When a generator retries a failed API call, the retry closure
captures the original State; if AppendContent had mutated the receiver in place,
the retry would observe corrupted partial state from the failed attempt instead
of a clean starting point. Immutability is achieved by value-copying the
receiver (ret := s), cloning shared slices (slices.Clone, make+copy) before
mutation, and never writing through shared map or slice references. Content
objects referenced by pointer (*Content) are treated as copy-on-write: any
modification (e.g., Merge) produces a new *Content rather than mutating the
original. Shared maps (FuncMap.m) and slices (stateWithFunctions.fns) are safe to
share across State instances because they are never mutated after construction.
`

type State interface {
	Contents() iter.Seq[*Content]
	AppendContent(*Content) (State, error)
	SystemPrompt() string
	Functions() iter.Seq[*Function]
	Flush() (State, error)
	Unwrap() State
}
