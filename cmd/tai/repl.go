package main

import "github.com/reusee/tai/flags"

type Repl bool

func (Module) Repl() Repl {
	return false
}

var _ flags.Flag = Repl(true)

func (r Repl) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := Repl(true)
	return &ret, args, nil
}

func (r Repl) Keys() map[string]string {
	return map[string]string{
		"-repl": "Start a Starlark REPL for interactive debugging",
	}
}
