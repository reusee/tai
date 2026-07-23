package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/modes"
)

const TheoryOfGoCommand = `
The "go" subcommand provides code generation for Go files by selecting the "go"
CodeProvider, which delegates to gocodes.CodeProvider. It reuses the full
codes.Generate pipeline — including dynamic context, immediate apply, shell and
continue blocks, and round statistics — by wiring codes.Module into the dscope
scope. The -repl flag enables a REPL mode that taps the debugs infrastructure
without running generation, useful for interactive debugging. This is the
Go-oriented counterpart to the "any" subcommand for general-purpose text file
generation, and succeeds the standalone gotai command.

When no subcommand is provided and the current directory is inside a Go module
(a go.mod file is found by walking up the directory tree), the "go" subcommand
is automatically selected as the default. This makes "tai" convenient to invoke
in Go projects without explicitly specifying the subcommand each time.
`

var GoCommand = Command{
	Defs: []any{
		modes.ForProduction(),
		dscope.Provide(codes.CodeProviderName("go")),
	},
	Main: func(
		generate codes.Generate,
		tap debugs.Tap,
		repl Repl,
	) {
		if repl {
			tap(context.Background(), "repl", map[string]any{})
			return
		}
		if err := generate(context.Background(), os.Stdout); err != nil {
			panic(err)
		}
	},
}

type InGoModule bool

func (Module) InGoModule() InGoModule {
	dir, err := os.Getwd()
	if err != nil {
		return false
	}
	return InGoModule(dirHasGoModule(dir))
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
