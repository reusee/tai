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
	cmds.Define("-unified", cmds.Func(func() {
		diff = codes.UnifiedDiff{}
	}))
	cmds.Define("-xml", cmds.Func(func() {
		diff = codes.XmlDiffHandler{}
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
		diff = codes.XmlDiffHandler{}
	}
	if err := diff.Apply(root, target); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Applied hunks successfully.")
}
