package main

import (
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
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

var defs []any
var mainFunc any

func init() {

	cmds.Define("hello", cmds.Func(func() {
		type Greetings string
		defs = []any{
			new(Greetings("hello, world!")),
		}
		mainFunc = func(
			greetings Greetings,
		) {
			fmt.Printf("%s\n", greetings)
		}
	}))

}

func main() {
	maybeRunInContainer()

	cmds.Execute(os.Args[1:])

	scope := dscope.New(dscope.Methods(new(Module))...)
	if mainFunc != nil {
		scope.Fork(defs...).Call(mainFunc)
	}
}
