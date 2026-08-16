package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/reusee/dscope"
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
	// includes the embedded schema and config globals; forking it here
	// keeps the configs package self-contained with its own default.
	scope = scope.Fork(taiconfigs.ConfigsLoader)

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

	command := scope.Get[Command]()
	if command.Main == nil {
		return
	}

	// Every generation command resolves its generators in a scope that
	// carries the interaction recorder as the generators-level
	// EventRecorder. The binding is forked here, once, so commands do not
	// wire it individually; generators record API-level events
	// (api_call, api_error) through their dscope-injected EventRecorder
	// instead of receiving the recorder through the context. See
	// generators.TheoryOfEventRecorder.
	scope = scope.Fork(eventRecorderDef)

	if bool(scope.Get[Tui]()) {
		runWithTUI(command, scope)
		return
	}

	scope.Fork(command.Defs...).Call(command.Main)
}
