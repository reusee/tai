package main

import "github.com/reusee/tai/flags"

type Repl bool

func (Module) Repl() Repl {
	return false
}

var _ flags.Flag = Repl(true)

func (r Repl) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return Repl(true), args, nil
}

func (r Repl) Keys() []string {
	return []string{"-repl"}
}
