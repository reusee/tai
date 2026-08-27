package main

import (
	"testing"
)

func TestGoCommandKeepsInteractiveNoHumanDefault(t *testing.T) {
	// Guard the -repl contract: GoCommand must not force NoHuman to true
	// in its Defs. Goal mode (pipeline.GoalRun) runs unconditionally, so a
	// forced NoHuman would only disable the -repl REPL session.
	// See TheoryOfGoCommand.
	for _, def := range GoCommand.Defs {
		if _, ok := def.(func() NoHuman); ok {
			t.Fatal("GoCommand must not provide a NoHuman def; -repl must stay usable")
		}
	}
}
