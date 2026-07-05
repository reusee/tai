package codes

import "github.com/reusee/tai/cmds"

const TheoryOfDynamicContext = `
Dynamic context allows the model to request additional files or network resources
mid-generation by emitting request-context blocks. This enables multi-round context
expansion but adds latency and complexity. When disabled, the model operates solely
with the context provided in the initial request, and three components are omitted
in tandem: the request-context system prompt section that teaches the model how to
emit request-context blocks, the BlockState decorator that intercepts model output
for block parsing, and the ProcessRequestContextBlocks call that fetches requested
resources. All three must be enabled or disabled together to maintain conceptual
integrity — teaching the model about a capability without parsing its output, or
parsing output without teaching the model, would be incoherent.
`

// DynamicContext controls whether request-context block support is enabled.
// When true, the system prompt includes request-context instructions, the
// state is wrapped with BlockState for block parsing, and
// ProcessRequestContextBlocks is called to fetch requested resources.
// When false, all three are omitted. See TheoryOfDynamicContext.
type DynamicContext bool

var dynamicContextFlag DynamicContext

func init() {
	cmds.Define("-dynamic-context", cmds.Func(func() {
		dynamicContextFlag = true
	}).Alias("-dyn"))
}

func (Module) DynamicContext() DynamicContext {
	return dynamicContextFlag
}

