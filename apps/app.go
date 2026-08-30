package apps

import (
	"fmt"

	"github.com/reusee/dscope"
)

const TheoryOfApps = `
An App is a self-contained application: a name, a usage description,
a main function, the dscope modules that build its base scope, and the
definitions layered on top. The type parameter is the main function's
type; New infers it from the function value, so the Main field stays
type-safe without spelling the function type at each use.

App unifies the two ways an application runs. Run builds a standalone
scope from Modules, layers Name and Defs, and calls Main. A host that
already built a scope — a command dispatcher, a display frontend —
composes Scope and Call instead: Scope layers the app's Defs onto the
host scope, Call invokes Main with scope injection. Scope must be
applied exactly once per run: each fork branch evaluates providers
independently, so layering the same defs twice evaluates side-effecting
providers twice.

App is also the subcommand mechanism. Its Keys and Handle signatures
match the command-line flag interface shape: Keys registers Name as the
selection key with Description as the usage text, and Handle selects
the app by forking a Runner. An app with an empty Description is a
default or internal app and registers no key. Runner is the type-erased
interface over App instantiations — mains of different function types
are different types, so the scope, flag parsing, and hosts exchange
selected apps as Runner values.

Interactive is a scope value, not an App field: an app that reads
multi-turn interactive input while running forks Interactive(true) into
its Defs, and a display frontend reads it from the app's scope like any
other configuration.
`

// App is a self-contained application: its name and usage description,
// the dscope modules that build its base scope, the definitions layered
// on top, and the main function the scope calls. The type parameter is
// the main function's type; New constructs an App without spelling the
// function type. See TheoryOfApps.
type App[Main any] struct {
	Name        Name
	Description string
	Modules     []dscope.Module
	Defs        []any
	Main        Main
}

func (a App[Main]) Run() {
	var defs []any
	for _, mod := range a.Modules {
		defs = append(defs, mod)
	}
	scope := dscope.New(defs...)
	scope = scope.Fork(&a.Name)
	scope = scope.Fork(a.Defs...)
	scope.Call(scope.Get[Main]())
}

// Runner is the type-erased interface to an App. Apps whose main
// functions have different types are different App instantiations; the
// scope, flag parsing, and hosts such as display frontends exchange
// selected apps as Runner values. See TheoryOfApps.
type Runner interface {
	// Run runs the app standalone, building a scope from its Modules.
	Run()
	// Scope layers the app's Defs onto base and returns the resulting
	// scope. Apply it exactly once per run; see TheoryOfApps.
	Scope(base dscope.Scope) dscope.Scope
	// Call invokes the app's main function with scope injection. The
	// scope must already carry the app's definitions (see Scope).
	Call(scope dscope.Scope)
	// Keys registers the app as a selectable subcommand keyed by its
	// name, mapping the name to its usage description. An app with an
	// empty description registers no key.
	Keys() map[string]string
	// Handle selects this app for the given key, returning a *Runner
	// definition that overrides the selected app in the scope.
	Handle(key string, args []string) (newDef any, remainArgs []string, err error)
}

// New returns an App with the given name, description, main function,
// and definitions. The type of main determines the App's type
// parameter, so the function value may be an anonymous function.
// See TheoryOfApps.
func New[Main any](name Name, description string, main Main, defs ...any) App[Main] {
	return App[Main]{
		Name:        name,
		Description: description,
		Main:        main,
		Defs:        defs,
	}
}

// Scope layers the app's Defs onto base and returns the resulting
// scope. It deliberately does not fork the app's Name: apps that need
// their name in the scope fork it through their own Defs, and Run —
// the standalone path — forks Name itself. See TheoryOfApps.
func (a App[Main]) Scope(base dscope.Scope) dscope.Scope {
	return base.Fork(a.Defs...)
}

var _ Runner = App[func()]{}

// Call invokes the app's main function in scope, which must already
// carry the app's definitions (see Scope). See TheoryOfApps.
func (a App[Main]) Call(scope dscope.Scope) {
	scope.Call(a.Main)
}

// Keys registers the app as a selectable subcommand keyed by its name,
// with Description as the usage text. An app with an empty Description
// — a default or internal app — registers no key. See TheoryOfApps.
func (a App[Main]) Keys() map[string]string {
	if a.Description == "" {
		return nil
	}
	return map[string]string{
		string(a.Name): a.Description,
	}
}

// Handle selects this app for the given key, returning a *Runner that
// overrides the selected app in the scope. See TheoryOfApps.
func (a App[Main]) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if key != string(a.Name) {
		return nil, args, fmt.Errorf("app %q does not handle key %q", a.Name, key)
	}
	var runner Runner = a
	return &runner, args, nil
}
