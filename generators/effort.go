package generators

import "github.com/reusee/tai/flags"

// EffortFlag is the reasoning effort level from the -effort flag.
// When non-empty, it overrides the spec's ReasoningEffort setting.
type EffortFlag = flags.Effort
