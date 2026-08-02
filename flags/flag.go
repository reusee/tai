package flags

const TheoryOfConfigFlagParity = `
Configuration options are accessible through both command-line flags and
configuration files (CUE). Flags provide per-invocation overrides, while config
files provide persistent defaults. The configs.Load function runs before
flags.Parse, so flag values always override config values. For composite types
(maps, lists), flags accumulate values through repeated invocation, while config
files specify values as structured lists. API keys are exempt from this parity
principle: they are config-only and environment-variable-only to avoid exposing
secrets in process command-line listings.
`

// Flag is the interface for command-line flag types. Each Flag type
// registers its keys and descriptions via Keys, and handles argument
// consumption via Handle.
type Flag interface {
	// Keys returns a map from each flag key to its human-readable
	// description. The description is displayed in usage output. Each
	// key must be unique across all Flag types in the scope; Parse
	// returns an error on duplicate key registrations.
	Keys() map[string]string
	// Handle consumes arguments and returns a def that is passed directly
	// to scope.Fork. The def may be a pointer to a typed value (e.g., &ret)
	// or a function that provides the value with injected dependencies.
	// Returning nil signals an error. remainArgs is the unconsumed argument
	// tail.
	Handle(key string, args []string) (newDef any, remainArgs []string, err error)
}
