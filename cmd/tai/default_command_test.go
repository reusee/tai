package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/gotools"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pathutil"
	"github.com/reusee/tai/pipeline/codetypes"
)

func TestDefaultCommandAutoDetection(t *testing.T) {
	// Module.Command auto-detects the default command from the Go-module
	// check: GoModuleCommand inside a Go module, AnyTextCommand outside
	// one. pathutil.FindGoModuleRoot finds the nearest go.mod walking up
	// the directory tree. See TheoryOfCommandAutoDetection.
	dir := t.TempDir()
	if _, ok := pathutil.FindGoModuleRoot(dir); ok {
		t.Fatal("empty temp dir must not be detected as a Go module")
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := pathutil.FindGoModuleRoot(sub); !ok {
		t.Fatal("go.mod in a parent directory must be detected")
	}
	keys := make(map[string]string)
	for _, app := range []apps.App{
		NextCommand,
		AICommand,
		PatchCommand,
		PingCommand,
		RecordCommand,
		GoModuleCommand,
		AnyTextCommand,
	} {
		for key, desc := range app.Keys() {
			keys[key] = desc
		}
	}
	if _, ok := keys["go"]; ok {
		t.Fatal("the go subcommand must be removed from Keys")
	}
	if _, ok := keys["any"]; ok {
		t.Fatal("the any subcommand must be removed from Keys")
	}
	inModule := Module{}.Command(true)
	if appMainPointer(inModule) != appMainPointer(GoModuleCommand) {
		t.Fatal("the default command inside a Go module must be GoModuleCommand")
	}
	outside := Module{}.Command(false)
	if appMainPointer(outside) != appMainPointer(AnyTextCommand) {
		t.Fatal("the default command outside a Go module must be AnyTextCommand")
	}
}

// TestGoModuleCommandKeepsGoPartsProvider pins the go command's parts
// provider type. The codes pipeline's Go-session gate — the Go-specific
// extra system prompts — keys on the provider satisfying
// gotools.PartsProvider, so a command-level wrapper would hide the type and
// silently drop those prompts. The load-failure fallback therefore lives
// inside the provider (gotools.TheoryOfGoLoadFallback).
// See TheoryOfCommandAutoDetection.
func TestGoModuleCommandKeepsGoPartsProvider(t *testing.T) {
	scope := dscope.New(new(Module)).Fork(modes.ForTest(t))
	scope = GoModuleCommand.Scope(scope)
	var provider codetypes.PartsProvider
	scope.Call(func(p codetypes.PartsProvider) {
		provider = p
	})
	if _, ok := provider.(gotools.PartsProvider); !ok {
		t.Fatalf("the go command's parts provider must satisfy gotools.PartsProvider, got %T", provider)
	}
}

// appMainPointer returns the code pointer of an app's main function,
// identifying which app an App value carries. The Main field is an any
// holding the function, so Elem unwraps the interface before reading
// the code pointer; nothing is executed. See apps.TheoryOfApps.
func appMainPointer(app apps.App) uintptr {
	return reflect.ValueOf(app).FieldByName("Main").Elem().Pointer()
}
