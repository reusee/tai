package cmds

import (
	"fmt"
	"testing"
)

func TestVar(t *testing.T) {
	a := Var[int]("foo")
	b := Var[string]("bar")
	GlobalExecutor.MustExecute([]string{
		"foo", "42",
		"bar", "bar",
	})
	if *a != 42 {
		t.Fatal()
	}
	if *b != "bar" {
		t.Fatal()
	}
}

func TestSwitch(t *testing.T) {
	foo := Switch("TestSwitch")
	GlobalExecutor.Execute([]string{
		"TestSwitch",
	})
	if *foo != true {
		t.Fatal()
	}
	GlobalExecutor.MustExecute([]string{
		"!TestSwitch",
	})
	if *foo != false {
		t.Fatal()
	}
}

func TestTypedVar(t *testing.T) {
	type Foo string
	v := Var[Foo]("TestTypedVar")
	GlobalExecutor.MustExecute([]string{
		"TestTypedVar", "bar",
	})
	if *v != "bar" {
		t.Fatal()
	}
}
