package flags

import (
	"fmt"
	"reflect"

	"github.com/reusee/dscope"
)

var flagType = reflect.TypeFor[Flag]()

// TheoryOfFlagParsing documents the design rationale for the flag parser.
// The parser resolves flags from the current scope state on each iteration,
// enabling accumulating flags (e.g. repeated -chat) to observe values produced
// by earlier iterations within the same parse pass.
const TheoryOfFlagParsing = `
flags parsing theory:
- Flag types are discovered from the initial scope and keyed by their Flag.Key
  identifier for argument matching.
- Each iteration resolves the current flag value from the live scope, enabling
  accumulating flags to observe values produced by earlier iterations within
  the same parse pass.
- A flag's Handle method transforms remaining args into a new value that is
  forked into the scope, preserving scope immutability.
`

func Parse(scope dscope.Scope, args []string) (dscope.Scope, error) {
	flagTypes := make(map[string]reflect.Type)
	for t := range scope.AllTypes() {
		if !t.Implements(flagType) {
			continue
		}
		flagValue, ok := scope.Get(t)
		if !ok {
			return dscope.Scope{}, fmt.Errorf("flag type not found in scope: %v", t)
		}
		flag := flagValue.Interface().(Flag)
		key := flag.Key()
		flagTypes[key] = t
	}

	for len(args) > 0 {
		key := args[0]
		t, ok := flagTypes[key]
		if !ok {
			return dscope.Scope{}, fmt.Errorf("unknown flag: %s", key)
		}
		flagValue, ok := scope.Get(t)
		if !ok {
			return dscope.Scope{}, fmt.Errorf("flag type not found in scope: %v", t)
		}
		flag := flagValue.Interface().(Flag)
		newValue, remainArgs, err := flag.Handle(args[1:])
		if err != nil {
			return dscope.Scope{}, err
		}
		if newValue == nil {
			return dscope.Scope{}, fmt.Errorf("flag %s returned nil value", key)
		}
		ptr := reflect.New(t)
		ptr.Elem().Set(reflect.ValueOf(newValue))
		scope = scope.Fork(
			ptr.Interface(),
		)
		args = remainArgs
	}

	return scope, nil
}
