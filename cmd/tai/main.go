package main

import (
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
)

var defs []any
var mainFunc any

func init() {

	cmds.Define("hello", cmds.Func(func() {
		type Greetings string
		defs = []any{
			new(Greetings("hello, world!")),
		}
		mainFunc = func(
			greetings Greetings,
		) {
			fmt.Printf("%s\n", greetings)
		}
	}))

}

func main() {
	cmds.Execute(os.Args[1:])

	scope := dscope.New(dscope.Methods(new(Module))...)
	if mainFunc != nil {
		scope.Fork(defs...).Call(mainFunc)
	}
}
