package cmds

import "testing"

func TestUsage(t *testing.T) {
	executor := NewExecutor()
	executor.Define("foo", Sub(map[string]*Command{
		"bar": Func(func() {
		}).Desc("BAR"),
		"baz": Sub(map[string]*Command{
			"qux": Func(func() {}).Desc("QUX"),
		}).Desc("BAZ"),
	}).Desc("FOO"))
	executor.PrintUsage()
}
