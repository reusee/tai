package generators

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

const TheoryOfConfidentialMode = `
Confidential mode restricts model selection to generators that guarantee
zero data retention. When enabled, any attempt to resolve a model whose
spec does not set ZeroDataRetention explicitly is rejected at generator
resolution time, before any API request is sent. The check runs inside
GetGenerator after the spec is fully resolved — redirects, variants,
aliases, and field inheritance are accounted for. Built-in model shortcuts
(flash, gemini, etc.) and the ollama shorthand never set ZeroDataRetention
and are therefore rejected in confidential mode; only user-defined
generators that opt into zero data retention are usable.
`

// ConfidentialMode, when enabled, restricts model selection to generators
// whose spec sets ZeroDataRetention. See TheoryOfConfidentialMode.
type ConfidentialMode bool

func (Module) ConfidentialMode() ConfidentialMode {
	return false
}

var _ flags.Flag = ConfidentialMode(false)

func (c ConfidentialMode) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := ConfidentialMode(true)
	return &ret, args, nil
}

func (c ConfidentialMode) Keys() map[string]string {
	return map[string]string{
		"-confidential": "Restrict model selection to zero-data-retention models",
	}
}

var _ configs.Config = ConfidentialMode(false)

func (c ConfidentialMode) ConfigPaths() []string {
	return []string{"confidential_mode"}
}

func (c ConfidentialMode) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := ConfidentialMode(b)
	return &ret, nil
}

// check returns an error when confidential mode is enabled and the spec
// does not set ZeroDataRetention to true.
func (c ConfidentialMode) check(spec Spec, name string) error {
	if !bool(c) {
		return nil
	}
	if spec.ZeroDataRetention == nil || !*spec.ZeroDataRetention {
		return fmt.Errorf("confidential mode: model %q does not support zero data retention", name)
	}
	return nil
}
