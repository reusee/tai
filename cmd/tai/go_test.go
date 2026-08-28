package main

import (
	"testing"
)

func TestGoModuleCommandKeepsInteractiveNoHumanDefault(t *testing.T) {
	// Guard the -repl contract: GoModuleCommand must not force NoHuman to
	// true in its Defs. Goal mode (pipeline.GoalRun) runs unconditionally,
	// so a forced NoHuman would only disable the -repl REPL session.
	// See TheoryOfGoModuleDefault.
	for _, def := range GoModuleCommand.Defs {
		if _, ok := def.(func() NoHuman); ok {
			t.Fatal("GoModuleCommand must not provide a NoHuman def; -repl must stay usable")
		}
	}
}
