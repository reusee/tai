package taivm

import "fmt"

type List struct {
	Elements  []any
	Immutable bool
}

func ListAppend(vm *VM, args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("append expects 1 argument")
	}
	l, ok := args[0].(*List)
	if !ok {
		return nil, fmt.Errorf("receiver must be list")
	}
	if l.Immutable {
		return nil, fmt.Errorf("list is immutable")
	}
	l.Elements = append(l.Elements, args[1])
	return nil, nil
}
