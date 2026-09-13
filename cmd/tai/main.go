package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/security"
	"github.com/reusee/tai/taiconfigs"
)

func main() {
	security.MaybeRunInContainer()

	scope := dscope.New(dscope.Methods(new(Module))...)

	// Override the default configs.Loader (provided by configs.Module)
	// with the tai-specific loader from taiconfigs. The taiconfigs loader
	// includes the embedded schema and config globals; forking the module
	// here keeps the configs package self-contained with its own default.
	scope = scope.Fork(new(taiconfigs.Module))

	// Register the selectable subcommand apps so flags.Parse lists their
	// names and dispatches the selected one: the registry carries each
	// app's selection key and usage description. See apps.TheoryOfApps.
	scope = scope.Fork(&commands)

	// Load config file values before parsing flags so that command-line
	// values can override config file values. configs.Load discovers all
	// types implementing configs.Config in the scope, reads their CUE
	// paths from the loader, and forks the scope with the resolved values.
	// See configs.Config and configs.Load.
	loader := scope.Get[configs.Loader]()
	scope, err := configs.Load(loader, scope)
	if err != nil {
		ce(err)
	}

	scope, err = flags.Parse(scope, os.Args[1:])
	if err != nil {
		if helpErr, ok := errors.AsType[*flags.HelpError](err); ok {
			fmt.Print(helpErr.Usage)
			return
		}
		ce(err)
	}

	app, ok := scope.TryGet[apps.App]()
	if !ok {
		return
	}

	// Layer the app's definitions onto the scope exactly once, before
	// anything reads them: each fork branch evaluates providers
	// independently, so layering the same defs twice would evaluate
	// side-effecting providers twice. See apps.TheoryOfApps.
	scope = app.Scope(scope)

	// Generator-level events (api_call, api_error) are captured by the
	// generators module's default EventRecorder — the scope's EventSink —
	// and the generation loop drains the sink into session-tree event
	// nodes. No command-side override is needed. See
	// generators.TheoryOfEventRecorder.
	if bool(scope.Get[Tui]()) {
		if bool(scope.Get[RecordBrowserEnabled]()) {
			// The record subcommand has a dedicated browser: the
			// generation TUI's tabs, model output, and logs do not apply
			// to it. See TheoryOfRecordBrowser.
			runRecordBrowser(scope)
			return
		}
		runWithTUI(app, scope)
		return
	}

	app.Call(scope)
}
