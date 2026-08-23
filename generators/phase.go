package generators

import "context"

// Phase is one step of a generation phase chain: it receives the previous
// state and returns the next phase (nil ends the chain), the new state,
// and an error. Phase chains are driven by callers such as the pipeline
// loop and records' analysis pass.
type Phase func(ctx context.Context, prev State) (Phase, State, error)

// PhaseBuilder wraps a continuation Phase into a new Phase, so phases
// compose into chains (e.g., generate -> chat).
type PhaseBuilder func(cont Phase) Phase
