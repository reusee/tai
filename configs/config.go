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
	// be found in config files. Paths are checked in order; the first
	// path that yields a non-zero value (as determined by HandleConfig)
	// wins. All paths are static strings — dynamic paths based on
	// runtime dependencies are not supported because ConfigPaths is
	// called on the zero value of the type.
	ConfigPaths() []string

	// HandleConfig receives the CUE path and the cue.Values from all
	// config file roots that contain that path. It returns the new
	// value for the type, or nil to indicate no change. The receiver
	// is the current value of the type in the scope, allowing
	// HandleConfig to implement "first non-zero wins" semantics by
	// checking whether the receiver is already non-zero.
	HandleConfig(path string, values []*cue.Value) (any, error)
}

type DynamicPathsConfig interface {
	Config
	ConfigPathsFunc() any
}
