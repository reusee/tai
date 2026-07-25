package codes

import (
	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

// Apply configs.Config implementation. See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Apply(true)

func (a Apply) ConfigPaths() []string {
	return []string{"apply"}
}

func (a Apply) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return Apply(b), nil
}

// Debug configs.Config implementation for the codes module.

var _ configs.Config = Debug(false)

func (d Debug) ConfigPaths() []string {
	return []string{"debug_codes"}
}

func (d Debug) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return Debug(b), nil
}

// DynamicContext configs.Config implementation.

var _ configs.Config = DynamicContext(false)

func (d DynamicContext) ConfigPaths() []string {
	return []string{"dynamic_context"}
}

func (d DynamicContext) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return DynamicContext(b), nil
}

// Plan configs.Config implementation.

var _ configs.Config = Plan(false)

func (p Plan) ConfigPaths() []string {
	return []string{"plan"}
}

func (p Plan) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return Plan(b), nil
}
