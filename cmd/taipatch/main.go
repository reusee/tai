package main

import (
	"fmt"
	"os"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/memlimit"
)

func main() {
	cmds.Execute(os.Args[1:])
	memlimit.ApplyFromFlag()

	target := ".AI"
	root, err := os.OpenRoot(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	var handler codes.BoundaryDiffHandler
	for hunk, err := range handler.Apply(root, target) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Applied %s %s\n", hunk.Op, hunk.Target)
	}
}
