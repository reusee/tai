package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/taitape"
)

var tapeFile = cmds.Var[string]("-file")

func main() {
	cmds.Execute(os.Args[1:])

	if *tapeFile == "" {
		fmt.Fprintln(os.Stderr, "Error: -file <tape.json> is required")
		os.Exit(1)
	}

	// Initialize Tape if not exists
	if _, err := os.Stat(*tapeFile); os.IsNotExist(err) {
		initial := taitape.Tape{
			PC:      0,
			Globals: make(map[string]any),
		}
		data, _ := json.MarshalIndent(initial, "", "  ")
		os.WriteFile(*tapeFile, data, 0644)
		fmt.Printf("Created initial tape file: %s\n", *tapeFile)
	}

	scope := dscope.New(
		new(taitape.Module),
		modes.ForProduction(),
	)

	scope.Call(func(
		logger logs.Logger,
	) {
		vm := &taitape.VM{
			FilePath: *tapeFile,
			Logger:   logger,
		}

		ctx := context.Background()
		if err := vm.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Execution halted: %v\n", err)
			os.Exit(1)
		}
	})
}