package configs

import (
	"reflect"

	"cuelang.org/go/cue"
	"github.com/reusee/dscope"
)

var configType = reflect.TypeFor[Config]()

// Load reads configuration values from the loader and forks the scope
// with the resolved values. It discovers all types implementing Config
// in the scope, looks up their CUE paths in the loader, and calls
// HandleConfig to produce new values.
//
// Load should be called before flags.Parse so that command-line values
// can override config file values. The call order in main is:
//
//	scope := dscope.New(...)     // Module methods provide defaults
//	scope, _ = configs.Load(...)  // config file values override defaults
//	scope, _ = flags.Parse(...)   // CLI flags override config values
func Load(loader Loader, scope dscope.Scope) (dscope.Scope, error) {
	// Discover all Config types and their paths. AllTypes iteration
	// order is non-deterministic, but this is acceptable: each Config
	// type is independent, and HandleConfig only depends on the
	// receiver (the type's own current value) and the cue values for
	// its own paths.
	type configEntry struct {
		typ   reflect.Type
		paths []string
	}
	var entries []configEntry
	for t := range scope.AllTypes() {
		if !t.Implements(configType) {
			continue
		}
		zero := reflect.Zero(t).Interface().(Config)

		var paths []string
		if dyn, ok := zero.(DynamicPathsConfig); ok {
			fn := dyn.ConfigPathsFunc()
			scope.Call(fn).Assign(&paths)
		} else {
			paths = zero.ConfigPaths()
		}

		entries = append(entries, configEntry{
			typ:   t,
			paths: paths,
		})
	}

	for _, entry := range entries {
		for _, path := range entry.paths {
			// Collect cue.Values from all roots for this path.
			var values []*cue.Value
			for v, err := range loader.IterCueValues(path) {
				if err != nil {
					return scope, err
				}
				values = append(values, v)
			}
			if len(values) == 0 {
				continue
			}

			// Get the current value from the scope. After a previous
			// path may have already set a value, the current value
			// reflects that, allowing HandleConfig to implement
			// "first non-zero wins" by checking the receiver.
			value, ok := scope.Get(entry.typ)
			if !ok {
				continue
			}
			config := value.Interface().(Config)

			newValue, err := config.HandleConfig(path, values)
			if err != nil {
				return scope, err
			}
			if newValue == nil {
				continue
			}

			// Fork the scope with the new value, mirroring flags.Parse.
			ptr := reflect.New(entry.typ)
			ptr.Elem().Set(reflect.ValueOf(newValue))
			scope = scope.Fork(ptr.Interface())
		}
	}

	return scope, nil
}
