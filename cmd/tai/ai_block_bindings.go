package main

import (
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/flags"
)

const TheoryOfAIBlockBindings = `
The ai command uses a subset of the block binding mechanism: shell and continue
blocks are processed in the generation loop, while memory blocks are processed
after the loop by memories.UpdateMemoryFromBlock. The memory block's prompt is
assembled inline in AISystemPrompt because it includes the dynamic user profile
text, which cannot be a static PromptSection.

Shell and continue bindings are reused from blocks.CommonBlockBindings, the
shared binding set constructed in the blocks package. The codes module also
reuses CommonBlockBindings, prepending its codes-specific bindings (change,
finish, request-context) and appending summary. This eliminates the duplicate
binding construction that previously existed when the ai command and codes
module each defined their own shell and continue bindings independently.

AIBlockBindings is a distinct named type embedding blocks.BlockBindings so that
dscope resolves it independently from the codes module's CodesBlockBindings
provider. Both the ai command and the codes module use distinct named types
embedding blocks.BlockBindings, ensuring each module's bindings are resolved
independently in the dscope scope without type conflicts.
`

// AIBlockBindings is the block bindings type for the ai command. It embeds
// blocks.BlockBindings as an anonymous struct field so that dscope can resolve
// it independently from the codes module's BlockBindings, avoiding a type
// conflict when both providers are wired into the same scope. Method
// promotion eliminates the need for explicit delegation methods.
// See TheoryOfAIBlockBindings.
type AIBlockBindings struct {
	blocks.BlockBindings
}

func (Module) AIBlockBindings(
	flagShell flags.Shell,
) (ret AIBlockBindings) {
	ret.BlockBindings = blocks.CommonBlockBindings(bool(flagShell))
	return
}
