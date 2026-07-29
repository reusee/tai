package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

const TheoryOfContainerIsolation = `
The tai command runs in a Linux user namespace (CLONE_NEWUSER, CLONE_NEWNS,
CLONE_NEWPID, CLONE_NEWUTS, and CLONE_NEWIPC) to isolate filesystem access,
preventing AI-driven code generation from writing outside the intended project
boundary. Network namespace is deliberately omitted because the AI pipeline
requires network access to call model APIs. The process re-executes itself in
the new namespace on first launch; the inContainerEnv environment variable marks
that the process is already containerized, ensuring re-execution happens only
once. On non-Linux platforms, container isolation is a no-op and the command
runs directly.

Filesystem hardening enforces a read-only-everything policy with targeted
writable exceptions. All mount points are remounted read-only, then the current
working directory is bind-mounted onto itself read-write, creating a writable
enclave for project file modifications. Go toolchain directories (GOCACHE,
GOMODCACHE, GOPATH/pkg) are resolved before namespace creation and individually
bind-mounted read-write so the Go toolchain can function (build cache, module
downloads, package objects). A fresh tmpfs is mounted on /tmp for isolated
temporary file storage, and on /dev/shm for isolated shared memory. /proc is
remounted to show only namespace-local processes, /sys is made read-only, and
sensitive /proc paths are masked with bind-mounted /dev/null. The NO_NEW_PRIVS
prctl flag prevents privilege escalation through exec, complementing the user
namespace's capability restrictions.
`

const inContainerEnv = "CAI_IN_CONTAINER"

func main() {
	maybeRunInContainer()

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
