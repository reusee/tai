package main

import (
	"fmt"
	"os"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes"
	"github.com/reusee/tai/codes/codetypes"
)

var diff codetypes.DiffHandler

func init() {
	cmds.Define("-boundary", cmds.Func(func() {
		diff = codes.BoundaryDiffHandler{}
	}))
}

func main() {
	cmds.Execute(os.Args[1:])

	target := ".AI"
	root, err := os.OpenRoot(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if diff == nil {
		diff = codes.BoundaryDiffHandler{}
	}
	for hunk, err := range diff.Apply(root, target) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Applied %s %s\n", hunk.Op, hunk.Target)
	}
}