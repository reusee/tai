package configs

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"
)

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
	// config file roots that contain that path. It returns a newDef —
	// a pointer to a typed value (e.g., &ret) or a function provider —
	// that is passed directly to scope.Fork, exactly like
	// flags.Flag.Handle's newDef. Returning nil indicates no change
	// for this path. The receiver is the original value of the type in
	// the scope (before any path processing), not the value from a
	// previous path. This ensures that later paths can override earlier
	// ones: HandleConfig should return a def derived from the current
	// path's cue.Values if they contain a meaningful value, regardless
	// of the receiver.
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

// AppendStringsConfig aggregates the config values of a string-list
// config type: each cue.Value contributes a string (empty strings are
// skipped) or a list of strings. It is the shared implementation of the
// additively aggregated extra-prompt Config types
// (flags.ExtraSystemPrompt, gotools.ExtraSystemPrompt), whose HandleConfig
// implementations were identical apart from the config path.
func AppendStringsConfig(current []string, values []*cue.Value) ([]string, error) {
	ret := slices.Clone(current)
	for _, v := range values {
		switch v.Kind() {
		case cue.StringKind:
			var s string
			if err := v.Decode(&s); err != nil {
				return nil, err
			}
			if s != "" {
				ret = append(ret, s)
			}
		case cue.ListKind:
			var list []string
			if err := v.Decode(&list); err != nil {
				return nil, err
			}
			ret = append(ret, list...)
		default:
			return nil, fmt.Errorf("expected string or list, got %v", v.Kind())
		}
	}
	return ret, nil
}

// AppendFamilyStringsConfig aggregates the config values of a
// map-of-string-lists config type: each cue.Value is a struct whose
// fields name families and whose values are a string (empty skipped)
// or a list of strings, all appended additively per family. It is the
// shared implementation of the additively aggregated family-prompt
// Config types (flags.FamilyExtraSystemPrompt,
// gotools.FamilyExtraSystemPrompt), whose HandleConfig implementations
// were identical apart from the config path.
func AppendFamilyStringsConfig(
	current map[string][]string,
	values []*cue.Value,
) (map[string][]string, error) {
	ret := make(map[string][]string, len(current))
	for family, prompts := range current {
		ret[family] = slices.Clone(prompts)
	}
	for _, v := range values {
		iter, err := v.Fields()
		if err != nil {
			return nil, err
		}
		for iter.Next() {
			family := iter.Selector().Unquoted()
			val := iter.Value()
			switch val.Kind() {
			case cue.StringKind:
				var s string
				if err := val.Decode(&s); err != nil {
					return nil, err
				}
				if s != "" {
					ret[family] = append(ret[family], s)
				}
			case cue.ListKind:
				var list []string
				if err := val.Decode(&list); err != nil {
					return nil, err
				}
				ret[family] = append(ret[family], list...)
			default:
				return nil, fmt.Errorf("expected string or list for family %q, got %v", family, val.Kind())
			}
		}
	}
	return ret, nil
}

// DecodeConfig decodes the first config value into T and returns a
// pointer to the typed result. It is the shared implementation of the
// scalar Config types' HandleConfig — booleans, strings, and integers —
// whose hand-written implementations were identical or behaviorally
// equivalent apart from the type name and minor stylistic drift
// (Decode vs String). Aggregate Config types use AppendStringsConfig
// and AppendFamilyStringsConfig; multi-value precedence semantics
// (e.g. flags.HandoffModel's first-non-empty rule) decode by hand.
func DecodeConfig[T any](values []*cue.Value) (*T, error) {
	var ret T
	if err := values[0].Decode(&ret); err != nil {
		return nil, err
	}
	return &ret, nil
}
