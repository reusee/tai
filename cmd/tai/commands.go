package main

import (
	"fmt"

	"github.com/reusee/tai/flags"
)

type Command struct {
	Defs []any
	Main any
}

func (Module) Command(
	inGoModule InGoModule,
) (ret Command) {
	if inGoModule {
		return GoCommand
	}
	return
}

var _ flags.Flag = Command{}

func (c Command) Keys() map[string]string {
	return map[string]string{
		"next":  "Identify and execute the most valuable next step",
		"ai":    "Start an interactive AI chat session with memory",
		"patch": "Apply a boundary-delimited diff file to the working tree",
		"go":    "Generate code for Go files (default in Go modules)",
		"any":   "Generate code for arbitrary text files",
	}
}

func (c Command) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	switch key {

	case "next":
		return NextCommand, args, nil

	case "ai":
		return AICommand, args, nil

	case "patch":
		return PatchCommand, args, nil

	case "go":
		return GoCommand, args, nil

	case "any":
		return AnyCommand, args, nil

	}

	panic(fmt.Errorf("command not handle: %s", key))
}
