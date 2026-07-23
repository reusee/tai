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

func (c Command) Keys() []string {
	return []string{
		"next",
		"ai",
		"patch",
		"go",
		"any",
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
