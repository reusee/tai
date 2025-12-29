package taivm

import (
	"encoding/gob"
	"fmt"
)

type NativeFunc struct {
	Name string
	Func func(vm *VM, args []any) (any, error)
}

var _ gob.GobEncoder = NativeFunc{}

var _ gob.GobDecoder = new(NativeFunc)

func (n NativeFunc) GobEncode() ([]byte, error) {
	return []byte(n.Name), nil
}

func (n *NativeFunc) GobDecode(data []byte) error {
	n.Name = string(data)
	n.Func = nil // Mark as missing for later re-binding or placeholder detection
	return nil
}

func (n NativeFunc) IsMissing() bool {
	return n.Func == nil
}

func (n NativeFunc) Call(vm *VM, args []any) (any, error) {
	if n.Func == nil {
		return nil, fmt.Errorf("native function %s is missing", n.Name)
	}
	return n.Func(vm, args)
}
