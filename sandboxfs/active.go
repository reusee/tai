package sandboxfs

import "sync/atomic"

const TheoryOfActiveStore = `
The process has at most one sandbox filesystem: the one the mount made the
process root. A session therefore never selects among stores — the store that
served the pivot is the environment, and every path the process touches
resolves through it. A process-wide record states that fact instead of
threading a store pointer through every consumer.

The record is set by the environment setup, after the mount succeeded and the
pivot completed, so a process whose sandbox mount failed carries no store and
every consumer degrades to "no sandbox" without a flag. The goal runner reads
it to apply each loop's recorded changes to the host (see
pipeline.TheoryOfLoopChangeApply); the review command does not read it,
because that command runs outside the sandbox root and opens the record's
directory or database file directly.

A store a test opens is not the process's environment, so Open and its
siblings do not record it: only the call that mounts and pivots does.
`

// activeStore is the store that serves the process root, or nil when the
// process runs without a sandbox filesystem. See TheoryOfActiveStore.
var activeStore atomic.Pointer[Store]

// SetActiveStore records the store that serves the process root. A nil store
// clears the record, which is what a process without a sandbox carries and
// what a test restores after it set one. See TheoryOfActiveStore.
func SetActiveStore(store *Store) {
	activeStore.Store(store)
}

// ActiveStore returns the store that serves the process root, or nil when the
// process runs without a sandbox filesystem. See TheoryOfActiveStore.
func ActiveStore() *Store {
	return activeStore.Load()
}
