package codes

import (
	"fmt"
	"slices"

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

// Review controls whether a review loop runs after the main generation
// loop (or after the goal command completes). When enabled, the changes
// made during generation are reviewed and corrected by one or more
// independent models. See TheoryOfReviewLoop.
type Review bool

// ReviewModels lists the models used for review, in order. Each model runs
// a separate review generation session with a fresh scope. When empty, the
// default generator is used once. See TheoryOfReviewLoop.
type ReviewModels []string

func (Module) ReviewModels() ReviewModels {
	return nil
}

var _ configs.Config = ReviewModels(nil)

var _ flags.Flag = ReviewModels(nil)

func (m ReviewModels) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := append(slices.Clone(m), args[0])
	return &ret, args[1:], nil
}

func (m ReviewModels) Keys() map[string]string {
	return map[string]string{
		"-review-model": "Set a review model to use for the review loop (repeatable, run in order)",
	}
}

func (m ReviewModels) ConfigPaths() []string {
	return []string{"review_models"}
}

func (m ReviewModels) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := slices.Clone(m)
	for _, v := range values {
		var list []string
		if err := v.Decode(&list); err != nil {
			return nil, err
		}
		ret = append(ret, list...)
	}
	return &ret, nil
}

func (Module) Review() Review {
	return false
}

var _ configs.Config = Review(false)

var _ flags.Flag = Review(true)

func (r Review) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := Review(true)
	return &ret, args, nil
}

func (r Review) Keys() map[string]string {
	return map[string]string{
		"-review": "Run a review loop after generation to review and fix changes",
	}
}

func (r Review) ConfigPaths() []string {
	return []string{"review"}
}

func (r Review) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := Review(b)
	return &ret, nil
}

func (Module) DynamicContext() DynamicContext {
	return false
}

// DynamicContext configs.Config implementation.
// See flags.TheoryOfConfigFlagParity.

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
