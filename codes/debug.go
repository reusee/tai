package codes

import "github.com/reusee/tai/flags"

type Debug bool

func (Module) Debug() Debug {
	return false
}

var _ flags.Flag = Debug(true)

func (d Debug) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return Debug(true), args, nil
}

func (d Debug) Keys() []string {
	return []string{"-debug-codes"}
}
