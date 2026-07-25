package configs

import (
	"reflect"

	"cuelang.org/go/cue"
	"github.com/reusee/dscope"
)

var configType = reflect.TypeFor[Config]()

const TheoryOfConfigPathPrecedence = `
Config path precedence theory:
- ConfigPaths and ConfigPathsFunc return ordered paths where later paths
  override earlier ones ("last non-zero wins"), not "first match wins".
- Load passes the original scope value (before any path processing) as the
  HandleConfig receiver for every path, preventing HandleConfig from
  detecting whether a previous path already set a value. This guarantees
  that later paths can always override earlier ones.
- HandleConfig should return the value from the current path's cue.Values
  if they contain a meaningful value, regardless of the receiver state.
- DynamicPathsConfig types are forked as provider functions (constructed
  via reflect.MakeFunc) rather than static values. The provider's
  parameters mirror the ConfigPathsFunc function's parameters, so dscope
  re-evaluates the config value when dependencies change. The provider
  captures the original scope value as the HandleConfig receiver and
  returns it unchanged when no config path yields a value, preserving
  Module-provided defaults.
`

// Load reads configuration values from the loader and forks the scope
// with the resolved values. It discovers all types implementing Config
// in the scope, looks up their CUE paths in the loader, and calls
// HandleConfig to produce new values.
//
// For regular Config types, the resolved value is forked as a static
// value. For DynamicPathsConfig types, Load constructs a provider
// function (via reflect.MakeFunc) whose parameters mirror the
// ConfigPathsFunc function's parameters; dscope re-evaluates the
// provider when its dependencies change, so the config value tracks
// dynamic path changes.
//
// Load should be called before flags.Parse so that command-line flags
// can override config file values. The call order in main is:
//
//	scope := dscope.New(...)     // Module methods provide defaults
//	scope, _ = configs.Load(...)  // config file values override defaults
//	scope, _ = flags.Parse(...)   // CLI flags override config values
func Load(loader Loader, scope dscope.Scope) (dscope.Scope, error) {
	// Discover all Config types and their paths. AllTypes iteration
	// order is non-deterministic, but this is acceptable: each Config
	// type is independent, and HandleConfig only depends on the
	// receiver (the type's own original value) and the cue values for
	// its own paths.
	type configEntry struct {
		typ       reflect.Type
		paths     []string
		pathFn    reflect.Value
		isDynamic bool
	}
	var entries []configEntry
	for t := range scope.AllTypes() {
		if !t.Implements(configType) {
			continue
		}
		zero := reflect.Zero(t).Interface().(Config)
		if dyn, ok := zero.(DynamicPathsConfig); ok {
			entries = append(entries, configEntry{
				typ:       t,
				pathFn:    reflect.ValueOf(dyn.ConfigPathsFunc()),
				isDynamic: true,
			})
		} else {
			entries = append(entries, configEntry{
				typ:   t,
				paths: zero.ConfigPaths(),
			})
		}
	}

	for _, entry := range entries {
		// Get the original value before processing any paths. The same
		// original value is used as the HandleConfig receiver for every
		// path, so HandleConfig cannot detect whether a previous path
		// already set a value. This guarantees "last non-zero wins"
		// semantics: later paths always override earlier ones.
		// See TheoryOfConfigPathPrecedence.
		value, ok := scope.Get(entry.typ)
		if !ok {
			continue
		}
		config := value.Interface().(Config)

		if entry.isDynamic {
			// For DynamicPathsConfig, fork a provider function (not a
			// static value) so dscope re-evaluates the config when
			// dependencies change.
			// See TheoryOfConfigPathPrecedence.
			provider := makeDynamicConfigProvider(loader, entry.typ, entry.pathFn, config, value)
			scope = scope.Fork(provider)
			continue
		}

		var err error
		scope, err = forkStaticConfigValues(loader, scope, entry.typ, entry.paths, config)
		if err != nil {
			return scope, err
		}
	}

	return scope, nil
}

// makeDynamicConfigProvider constructs a provider function for a
// DynamicPathsConfig type using reflect.MakeFunc. The provider's
// parameters mirror those of the ConfigPathsFunc function, so dscope
// re-evaluates the config value when dependencies change. When no
// config path yields a value, the provider returns the original scope
// value, preserving Module-provided defaults.
// See TheoryOfConfigPathPrecedence.
func makeDynamicConfigProvider(
	loader Loader,
	typ reflect.Type,
	pathFn reflect.Value,
	config Config,
	originalValue reflect.Value,
) any {
	pathFnType := pathFn.Type()
	paramTypes := make([]reflect.Type, pathFnType.NumIn())
	for i := range paramTypes {
		paramTypes[i] = pathFnType.In(i)
	}
	providerType := reflect.FuncOf(paramTypes, []reflect.Type{typ}, false)

	providerFn := reflect.MakeFunc(providerType, func(args []reflect.Value) []reflect.Value {
		// Resolve paths dynamically using the resolved dependencies.
		paths := pathFn.Call(args)[0].Interface().([]string)

		// Process paths in order; later non-nil results override
		// earlier ones ("last non-zero wins").
		var result any
		for _, path := range paths {
			var values []*cue.Value
			for v, err := range loader.IterCueValues(path) {
				if err != nil {
					panic(err)
				}
				values = append(values, v)
			}
			if len(values) == 0 {
				continue
			}
			newValue, err := config.HandleConfig(path, values)
			if err != nil {
				panic(err)
			}
			if newValue == nil {
				continue
			}
			result = newValue
		}

		// If no config path yielded a value, return the original
		// scope value to preserve the Module-provided default.
		if result == nil {
			return []reflect.Value{originalValue}
		}
		ptr := reflect.New(typ)
		ptr.Elem().Set(reflect.ValueOf(result))
		return []reflect.Value{ptr.Elem()}
	})

	return providerFn.Interface()
}

// forkStaticConfigValues processes static ConfigPaths for a regular
// Config type. For each path, it collects cue.Values from all loader
// roots and calls HandleConfig. Non-nil results are forked into the
// scope as static values, with later paths overriding earlier ones.
// See TheoryOfConfigPathPrecedence.
func forkStaticConfigValues(
	loader Loader,
	scope dscope.Scope,
	typ reflect.Type,
	paths []string,
	config Config,
) (dscope.Scope, error) {
	for _, path := range paths {
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

		newValue, err := config.HandleConfig(path, values)
		if err != nil {
			return scope, err
		}
		if newValue == nil {
			continue
		}

		// Fork the scope with the new value, mirroring flags.Parse.
		ptr := reflect.New(typ)
		ptr.Elem().Set(reflect.ValueOf(newValue))
		scope = scope.Fork(ptr.Interface())
	}
	return scope, nil
}
