package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/reusee/tai/pathutil"
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
	keys := Command{}.Keys()
	if _, ok := keys["go"]; ok {
		t.Fatal("the go subcommand must be removed from Keys")
	}
	if _, ok := keys["any"]; ok {
		t.Fatal("the any subcommand must be removed from Keys")
	}
	inModule := Module{}.Command(true)
	if inModule.Main == nil ||
		reflect.ValueOf(inModule.Main).Pointer() != reflect.ValueOf(GoModuleCommand.Main).Pointer() {
		t.Fatal("the default command inside a Go module must be GoModuleCommand")
	}
	outside := Module{}.Command(false)
	if outside.Main == nil ||
		reflect.ValueOf(outside.Main).Pointer() != reflect.ValueOf(AnyTextCommand.Main).Pointer() {
		t.Fatal("the default command outside a Go module must be AnyTextCommand")
	}
}
