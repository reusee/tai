package main

import (
	"os"
	"path/filepath"
)

// defaultToGoCommand checks if no subcommand was provided (mainFunc is nil)
// and the current directory is inside a Go module. If both conditions are
// met, it sets up the "go" subcommand as the default by calling
// setupGoCommand.
// See TheoryOfGoCommand in go.go.
func defaultToGoCommand() {
	if mainFunc == nil && inGoModule() {
		setupGoCommand()
	}
}

// inGoModule reports whether the current working directory is inside a Go
// module by walking up the directory tree looking for a go.mod file.
func inGoModule() bool {
	dir, err := os.Getwd()
	if err != nil {
		return false
	}
	return dirHasGoModule(dir)
}

// dirHasGoModule walks up the directory tree from dir looking for a go.mod
// file. It returns true if one is found, false if the filesystem root is
// reached without finding one.
func dirHasGoModule(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
