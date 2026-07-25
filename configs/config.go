package configs

import "cuelang.org/go/cue"

// Config is the interface for types that read values from configuration
// files. It is analogous to flags.Flag: each Config type registers CUE
// paths (instead of flag keys) and handles cue.Value arguments (instead
// of string args). Unlike Flag, there is no remainArgs because config
// values are read directly from structured files, not parsed from a
// command-line token stream.
//
// configs.Load discovers all Config types in a dscope scope, looks up
// each registered CUE path in the loader, and calls HandleConfig with
// the matching cue.Values from all config file roots. The returned
// value is forked into the scope, overriding the default provided by
// the type's Module method.
//
// Load should be called before flags.Parse so that command-line flags
// can override config file values.
type Config interface {
	// ConfigPaths returns the CUE paths at which this type's value may
	// be found in config files. Paths are checked in order; later paths
	// override earlier ones. If a path yields a non-nil value from
	// HandleConfig, it replaces the value from any previous path.
	// All paths are static strings — dynamic paths based on
	// runtime dependencies are not supported because ConfigPaths is
	// called on the zero value of the type.
	ConfigPaths() []string

	// HandleConfig receives the CUE path and the cue.Values from all
	// config file roots that contain that path. It returns the new
	// value for the type, or nil to indicate no change. The receiver
	// is the original value of the type in the scope (before any path
	// processing), not the value from a previous path. This ensures
	// that later paths can override earlier ones: HandleConfig should
	// return the value from the current path's cue.Values if they
	// contain a meaningful value, regardless of the receiver.
	HandleConfig(path string, values []*cue.Value) (any, error)
}

// DynamicPathsConfig extends Config for types whose CUE paths depend on
// other scope values. Instead of resolving paths once during Load and
// forking a static value, Load constructs a provider function (via
// reflect.MakeFunc) whose parameters mirror the ConfigPathsFunc
// function's parameters. dscope re-evaluates the provider when its
// dependencies change, so the config value tracks dynamic path changes.
type DynamicPathsConfig interface {
	Config
	ConfigPathsFunc() any
}
