package flags

import (
	"fmt"
	"maps"
)

type Files map[string]bool

func (Module) Files() (ret Files) {
	return
}

var _ Flag = Files(nil)

func (f Files) Keys() map[string]string {
	return map[string]string{
		"-file": "Add a file to the context by path or glob pattern",
	}
}

func (f Files) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	// Copy the existing map to preserve scope immutability; dscope.Get
	// returns the same map reference stored in the scope, so mutating it
	// in place would violate the immutable-scope contract.
	ret := make(Files, len(f)+1)
	maps.Copy(ret, f)
	ret[args[0]] = true
	newValue = ret
	remainArgs = args[1:]
	return
}
