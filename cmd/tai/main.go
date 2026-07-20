package main

import (
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
)

var mainFunc any

func init() {
	cmds.Define("hello", cmds.Func(func() {
		mainFunc = func() {
			fmt.Printf("hello, world!\n")
		}
	}))
}

func main() {
	cmds.Execute(os.Args[1:])

	scope := dscope.New(dscope.Methods(new(Module))...)
	if mainFunc != nil {
		scope.Call(mainFunc)
	}
}
