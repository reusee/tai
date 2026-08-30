package apps

import (
	"fmt"

	"github.com/reusee/dscope"
)

const TheoryOfApps = `
An App is a self-contained application: a name, a usage description,
a main function, the dscope modules that build its base scope, and the
definitions layered on top. Main holds the function value untyped: the
scope resolves its parameters at call time, so one concrete App type
describes every application and New takes the function value as-is.

App unifies the two ways an application runs. Run builds a standalone
scope from Modules, layers Name and Defs, and calls Main. A host that
already built a scope — a command dispatcher, a display frontend —
composes Scope and Call instead: Scope layers the app's Defs onto the
host scope, Call invokes Main with scope injection. Scope must be
applied exactly once per run: each fork branch evaluates providers
independently, so layering the same defs twice evaluates side-effecting
providers twice.

Apps is the subcommand mechanism. A host holds the selectable
subcommands as one Apps registry definition in the scope; Apps carries
the command-line flag interface shape: Keys merges every app's
selection key with Description as the usage text, and Handle selects
the app whose name matches the key, returning an *App definition that
overrides the selected app in the scope. An app with an empty
Description is a default or internal app and registers no key. The
selected app is a scope definition of type App — the host's default
provider supplies it and Handle's *App overrides it — so flag parsing
and hosts such as display frontends exchange the selection as a plain
App value.

Interactive is a scope value, not an App field: an app that reads
multi-turn interactive input while running forks Interactive(true) into
its Defs, and a display frontend reads it from the app's scope like any
other configuration.
`

// App is a self-contained application: its name and usage description,
// the dscope modules that build its base scope, the definitions layered
// on top, and the main function the scope calls. Main holds any
// function value; New constructs an App without spelling the function
// type. See TheoryOfApps.
type App struct {
	Name        Name
	Description string
	Modules     []dscope.Module
	Defs        []any
	Main        any
}

func (a App) Run() {
	var defs []any
	for _, mod := range a.Modules {
		defs = append(defs, mod)
	}
	scope := dscope.New(defs...)
	scope = scope.Fork(&a.Name)
	scope = scope.Fork(a.Defs...)
	scope.Call(a.Main)
}

// Apps is the registry of selectable subcommand apps, held in a scope
// as one definition. It carries the command-line flag interface shape:
// Keys merges every app's selection key, and Handle selects the app
// whose name matches the key, returning an *App definition that
// overrides the selected app in the scope. See TheoryOfApps.
type Apps []App

// Keys registers every selectable app as a subcommand keyed by its
// name, with Description as the usage text. Apps with an empty
// Description — defaults or internal apps — register no key.
// See TheoryOfApps.
func (as Apps) Keys() map[string]string {
	keys := make(map[string]string)
	for _, a := range as {
		for key, desc := range a.Keys() {
			keys[key] = desc
		}
	}
	return keys
}

// Handle selects the app whose name matches the key and returns its
// *App definition, overriding the selected app in the scope.
// See TheoryOfApps.
func (as Apps) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	for i := range as {
		if newDef, remainArgs, err = as[i].Handle(key, args); err == nil {
			return newDef, remainArgs, nil
		}
	}
	return nil, args, fmt.Errorf("no app handles key %q", key)
}

// New returns an App with the given name, description, main function,
// and definitions. The main function value may be an anonymous
// function. See TheoryOfApps.
func New(name Name, description string, main any, defs ...any) App {
	return App{
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
func (a App) Scope(base dscope.Scope) dscope.Scope {
	return base.Fork(a.Defs...)
}

// Call invokes the app's main function in scope, which must already
// carry the app's definitions (see Scope). See TheoryOfApps.
func (a App) Call(scope dscope.Scope) {
	scope.Call(a.Main)
}

// Keys registers the app as a selectable subcommand keyed by its name,
// with Description as the usage text. An app with an empty Description
// — a default or internal app — registers no key. See TheoryOfApps.
func (a App) Keys() map[string]string {
	if a.Description == "" {
		return nil
	}
	return map[string]string{
		string(a.Name): a.Description,
	}
}

// Handle selects this app for the given key, returning an *App that
// overrides the selected app in the scope. See TheoryOfApps.
func (a App) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if key != string(a.Name) {
		return nil, args, fmt.Errorf("app %q does not handle key %q", a.Name, key)
	}
	return &a, args, nil
}
