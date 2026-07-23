package flags

import (
	"fmt"
	"slices"
)

type Focus []string

func (Module) Focus() (ret Focus) {
	return
}

var _ Flag = Focus(nil)

func (f Focus) Keys() []string {
	return []string{"-focus"}
}

func (f Focus) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	newValue = append(slices.Clone(f), args[0])
	remainArgs = args[1:]
	return
}
