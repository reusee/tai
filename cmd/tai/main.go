package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
)

const TheoryOfContainerIsolation = `
The tai command runs in a Linux user namespace (CLONE_NEWUSER and CLONE_NEWNS)
to isolate filesystem access, preventing AI-driven code generation from writing
outside the intended project boundary. The process re-executes itself in the
new namespace on first launch; the inContainerEnv environment variable marks
that the process is already containerized, ensuring re-execution happens only
once. On non-Linux platforms, container isolation is a no-op and the command
runs directly.
`

const inContainerEnv = "CAI_IN_CONTAINER"

func main() {
	maybeRunInContainer()

	scope := dscope.New(dscope.Methods(new(Module))...)

	scope, err := flags.Parse(scope, os.Args[1:])
	if err != nil {
		var helpErr *flags.HelpError
		if errors.As(err, &helpErr) {
			fmt.Print(helpErr.Usage)
			return
		}
		ce(err)
	}

	command := dscope.Get[Command](scope)
	if command.Main != nil {
		scope.Fork(command.Defs...).Call(command.Main)
	}

}
