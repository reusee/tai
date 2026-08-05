package codes

import (
	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

const TheoryOfDynamicContext = `
Dynamic context allows the model to request additional files or network resources
mid-generation by emitting request-context blocks. When enabled, the system prompt
includes request-context instructions, the state is wrapped with ParserState for
block parsing, and ProcessRequestContextBlocks is called to fetch requested resources.
When disabled, all three are omitted. The ParserState decorator that intercepts model
output for block parsing is shared infrastructure: it is activated when either dynamic
context or immediate apply is enabled, because both features parse structured blocks
from streamed output.
`

// DynamicContext controls whether request-context block support is enabled.
// When true, the system prompt includes request-context instructions, the
// state is wrapped with ParserState for block parsing, and
// ProcessRequestContextBlocks is called to fetch requested resources.
// When false, all three are omitted. See TheoryOfDynamicContext.
type DynamicContext bool

func (Module) DynamicContext() DynamicContext {
	return false
}

// DynamicContext configs.Config implementation.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = DynamicContext(false)

var _ flags.Flag = DynamicContext(true)

func (d DynamicContext) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return new(DynamicContext(true)), args, nil
}

func (d DynamicContext) Keys() map[string]string {
	return map[string]string{
		"-dynamic-context": "Enable dynamic context fetching via request-context blocks",
		"-dyn":             "Alias for -dynamic-context: enable dynamic context fetching",
	}
}

func (d DynamicContext) ConfigPaths() []string {
	return []string{"dynamic_context"}
}

func (d DynamicContext) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := DynamicContext(b)
	return &ret, nil
}
