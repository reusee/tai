package main

import (
	"fmt"

	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/sandboxfs"
)

const TheoryOfSandboxLoopApplyBinding = `
The per-loop apply is bound at the composition root, not by a module method.
pipeline.Module provides the nil default for pipeline.LoopChangeApply, and the
process's sandbox store must replace it. A cmd/tai.Module method returning the
same type does not replace the default: method discovery descends into the
module-typed fields cmd/tai.Module holds, so the embedded pipeline module's
provider and the outer struct's method would both produce
pipeline.LoopChangeApply, and one fork rejects two definitions of one output
type. main forks the definition instead, beside the config loader and the
subcommand registry it overrides for the same reason.

The binding reads the process's active store when the hook runs, not when the
definition resolves. The sandbox mount happens before the scope exists, so a
captured store would have to be set through the scope; the record is also set
and cleared within one process by tests, so a captured pointer would go stale.
A nil record — no sandbox, a failed mount, the review command — yields an
inert hook, so a run outside the sandbox applies nothing and pays nothing. See
pipeline.TheoryOfLoopChangeApply and sandboxfs.TheoryOfActiveStore.
`

// sandboxLoopChangeApply binds the process's active sandbox store to the goal
// runner's per-loop apply: every loop's recorded changes reach the host before
// the next loop reads the filesystem, so the run's work is durable as it
// proceeds. It is a plain function so tests call it directly; main forks it as
// the pipeline.LoopChangeApply definition. See TheoryOfSandboxLoopApplyBinding.
func sandboxLoopChangeApply() pipeline.LoopChangeApply {
	return func(loop int) (int, []string, error) {
		store := sandboxfs.ActiveStore()
		if store == nil {
			return 0, nil, nil
		}
		applied, conflicts, err := store.ApplyAll()
		kept := make([]string, 0, len(conflicts))
		for _, c := range conflicts {
			kept = append(kept, fmt.Sprintf("%s %s: %s", c.Kind, c.Path, c.Conflict))
		}
		return len(applied), kept, err
	}
}
