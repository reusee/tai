package tailang

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
	n.Func = func(vm *VM, args []any) (any, error) {
		return nil, fmt.Errorf("native function %s is missing", n.Name)
	}
	return nil
}
