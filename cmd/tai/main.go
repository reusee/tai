package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/pathutil"
	"github.com/reusee/tai/security"
	"github.com/reusee/tai/taiconfigs"
)

func main() {
	// The tai dir is resolved once, before the container layer starts: the
	// sandbox makes it writable so a session in a module subdirectory may
	// write the tree's .tai, and the re-executed child reads the value from
	// the environment instead of re-deriving it after the mounts hardened.
	// The same value enters the scope below, so every package that persists
	// state writes the directory the sandbox allows.
	// See pathutil.TheoryOfTaiDir and security.TheoryOfWritableDirs.
	taiDir := pathutil.ResolveTaiDir()
	if filepath.IsAbs(string(taiDir)) {
		security.SetTaiDir(string(taiDir))
	}

	security.MaybeRunInContainer()

	scope := dscope.New(dscope.Methods(new(Module))...)

	// Pin the resolved tai dir: one resolution serves the sandbox and the
	// scope, so a state file and the writable directory never disagree.
	// See pathutil.TheoryOfTaiDir.
	scope = scope.Fork(func() pathutil.TaiDir {
		return taiDir
	})

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
