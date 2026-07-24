package taiconfigs

import "github.com/reusee/prompts"

const TheoryOfConfigGlobalsInjection = `
ConfigGlobals provides Go values that are injected into the CUE evaluation
context as global references. Config files can reference these values by
their key path (e.g., prompts.fiction references a Go string injected under
the "prompts" key). The default provider returns an empty map; modules that
want to inject values override this provider on their own Module struct with
a method that builds the globals map from Go constants, variables, or
computed values. In Go, when an outer struct and an embedded struct both
define a method with the same name, the outer struct's method shadows the
embedded one, so the override takes precedence without conflict.
`

// ConfigGlobals provides Go values injected into the CUE evaluation context
// as global references. Config files can reference these values by their key
// path (e.g., prompts.fiction). The default provider returns an empty map;
// modules that want to inject values provide their own ConfigGlobals method
// on their Module struct, which shadows this default via Go method promotion.
// See TheoryOfConfigGlobalsInjection.
type ConfigGlobals map[string]any

func (Module) ConfigGlobals() ConfigGlobals {
	return ConfigGlobals{
		"prompts": map[string]any{
			"fiction": prompts.Fiction,
			"codes":   prompts.Codes,
			"next":    prompts.NextStep,
		},
	}
}
