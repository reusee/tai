package flags

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/reusee/dscope"
)

var flagType = reflect.TypeFor[Flag]()

// TheoryOfFlagParsing documents the design rationale for the flag parser.
// The parser resolves flags from the current scope state on each iteration,
// enabling accumulating flags (e.g. repeated chat) to observe values produced
// by earlier iterations within the same parse pass.
const TheoryOfFlagParsing = `
flags parsing theory:
- Flag types are discovered from the initial scope and keyed by their Flag.Keys
  identifiers for argument matching. Each key maps to a human-readable
  description used in usage output.
- Duplicate key detection prevents two flag types from registering the same
  key, which would cause ambiguous argument matching. Parse returns an error
  listing both conflicting types.
- Each iteration resolves the current flag value from the live scope, enabling
  accumulating flags to observe values produced by earlier iterations within
  the same parse pass.
- A flag's Handle method receives the matched key so flags with multiple keys
  (e.g. shell/no-shell) can distinguish invocations, and transforms remaining
  args into a new value that is forked into the scope, preserving scope
  immutability.
- Help keys (-help, --help, -h) are checked before the main parse loop. When
  detected, Parse returns a HelpError carrying the formatted usage string so
  the caller can display it without re-scanning the scope.
- When an unknown flag is encountered, the error message includes the full
  usage listing so the user can see all available flags.
`

// ErrHelp is returned by Parse when a help flag (-help, --help, or -h) is
// detected in the arguments. The caller should check for this error via
// errors.As with *HelpError and display the Usage field.
var ErrHelp = errors.New("help requested")

// HelpError carries the formatted usage string when a help flag is detected
// during Parse. The caller should print the Usage field and exit normally.
type HelpError struct {
	Usage string
}

func (e *HelpError) Error() string {
	return "help requested"
}

// Is implements errors.Is so that errors.Is(err, ErrHelp) returns true for
// HelpError values.
func (e *HelpError) Is(target error) bool {
	return target == ErrHelp
}

// helpKeys is the set of arguments that trigger help output.
var helpKeys = map[string]bool{
	"-help":  true,
	"--help": true,
	"-h":     true,
}

func Parse(scope dscope.Scope, args []string) (dscope.Scope, error) {
	flagTypes := make(map[string]reflect.Type)
	flagDescriptions := make(map[string]string)
	for t := range scope.AllTypes() {
		if !t.Implements(flagType) {
			continue
		}
		flagValue, ok := scope.Get(t)
		if !ok {
			return dscope.Scope{}, fmt.Errorf("flag type not found in scope: %v", t)
		}
		flag := flagValue.Interface().(Flag)
		for key, desc := range flag.Keys() {
			if existing, dup := flagTypes[key]; dup {
				return dscope.Scope{}, fmt.Errorf("duplicate flag key %q registered by both %v and %v", key, existing, t)
			}
			flagTypes[key] = t
			flagDescriptions[key] = desc
		}
	}

	// Add help keys to descriptions so they appear in usage output.
	for k := range helpKeys {
		flagDescriptions[k] = "Show available flags and their descriptions"
	}

	// Check for help before the main parse loop. When a help key is found,
	// return a HelpError carrying the formatted usage string so the caller
	// can display it without re-scanning the scope.
	for _, arg := range args {
		if helpKeys[arg] {
			return scope, &HelpError{Usage: FormatUsage(flagDescriptions)}
		}
	}

	for len(args) > 0 {
		key := args[0]
		t, ok := flagTypes[key]
		if !ok {
			return dscope.Scope{}, fmt.Errorf("unknown flag: %s\n\n%s", key, FormatUsage(flagDescriptions))
		}
		flagValue, ok := scope.Get(t)
		if !ok {
			return dscope.Scope{}, fmt.Errorf("flag type not found in scope: %v", t)
		}
		flag := flagValue.Interface().(Flag)
		newValue, remainArgs, err := flag.Handle(key, args[1:])
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

// FormatUsage returns a formatted string listing all available flags and their
// descriptions, sorted alphabetically by key.
func FormatUsage(descriptions map[string]string) string {
	keys := make([]string, 0, len(descriptions))
	for k := range descriptions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("Available flags:\n")
	for _, k := range keys {
		desc := descriptions[k]
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&sb, "  %s\t%s\n", k, desc)
	}
	return sb.String()
}

// Usage collects all flag descriptions from the scope and returns a formatted
// usage string. It can be called to display available flags to the user.
func Usage(scope dscope.Scope) string {
	descriptions := make(map[string]string)
	for t := range scope.AllTypes() {
		if !t.Implements(flagType) {
			continue
		}
		flagValue, ok := scope.Get(t)
		if !ok {
			continue
		}
		flag := flagValue.Interface().(Flag)
		for key, desc := range flag.Keys() {
			descriptions[key] = desc
		}
	}
	// Add help keys so they appear in usage output.
	for k := range helpKeys {
		descriptions[k] = "Show available flags and their descriptions"
	}
	return FormatUsage(descriptions)
}
