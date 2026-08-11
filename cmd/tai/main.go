package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/security"
)

func main() {
	security.MaybeRunInContainer()

	scope := dscope.New(dscope.Methods(new(Module))...)

	// Load config file values before parsing flags so that command-line
	// values can override config file values. configs.Load discovers all
	// types implementing configs.Config in the scope, reads their CUE
	// paths from the loader, and forks the scope with the resolved values.
	// See configs.Config and configs.Load.
	loader := dscope.Get[configs.Loader](scope)
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

	command := dscope.Get[Command](scope)
	if command.Main == nil {
		return
	}

	if bool(dscope.Get[Tui](scope)) {
		runWithTUI(command, scope)
		return
	}

	scope.Fork(command.Defs...).Call(command.Main)
}
