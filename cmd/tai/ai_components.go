package main

import (
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
)

const TheoryOfAIComponents = `
The ai command uses a subset of the Component mechanism: shell and continue
components are processed in the generation loop, while memory blocks are
processed after the loop by memories.UpdateMemoryFromBlock. The memory block's
prompt is assembled inline in AISystemPrompt because it includes the dynamic
user profile text, which cannot be a static PromptSection.

Shell and continue components are reused from components.CommonComponents, the
shared component set constructed in the components package. The codes module
also reuses CommonComponents, prepending its codes-specific components (change,
finish, request-context, read-only files, mandatory planning) and appending
summary. This eliminates the duplicate component construction that previously
existed when the ai command and codes module each defined their own shell and
continue components independently.

AIComponents is a distinct named type embedding components.ComponentSet so that
dscope resolves it independently from the codes module's CodesComponents
provider. Both the ai command and the codes module use distinct named types
embedding components.ComponentSet, ensuring each module's components are
resolved independently in the dscope scope without type conflicts.
`

// AIComponents is the component set type for the ai command. It embeds
// components.ComponentSet as an anonymous struct field so that dscope can
// resolve it independently from the codes module's CodesComponents, avoiding
// a type conflict when both providers are wired into the same scope. Method
// promotion eliminates the need for explicit delegation methods.
// See TheoryOfAIComponents.
type AIComponents struct {
	components.ComponentSet
}

func (Module) AIComponents(
	flagShell flags.Shell,
) (ret AIComponents) {
	ret.ComponentSet = components.CommonComponents(bool(flagShell))
	return
}
