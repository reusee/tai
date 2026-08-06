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
		"next":   "Identify and execute the most valuable next step",
		"ai":     "Start an interactive AI chat session with memory",
		"patch":  "Apply a boundary-delimited diff file to the working tree",
		"go":     "Generate code for Go files (default in Go modules)",
		"any":    "Generate code for arbitrary text files",
		"ping":   "Test whether a model is reachable by sending a hello message",
		"goal":   "Work toward a goal through multiple independent generation loops",
		"record": "Record interaction sessions and analyze them for self-improvement",
	}
}

func (c Command) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	switch key {

	case "next":
		ret := NextCommand
		return &ret, args, nil

	case "ai":
		ret := AICommand
		return &ret, args, nil

	case "patch":
		ret := PatchCommand
		return &ret, args, nil

	case "go":
		ret := GoCommand
		return &ret, args, nil

	case "any":
		ret := AnyCommand
		return &ret, args, nil

	case "ping":
		ret := PingCommand
		return &ret, args, nil

	case "goal":
		ret := GoalCommand
		return &ret, args, nil

	case "record":
		ret := RecordCommand
		return &ret, args, nil

	}

	panic(fmt.Errorf("command not handle: %s", key))
}
