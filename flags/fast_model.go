package flags

import "fmt"

type FastModelName string

func (Module) FastModelName() (ret FastModelName) {
	return
}

var _ Flag = FastModelName("")

func (m FastModelName) Keys() map[string]string {
	return map[string]string{
		"-fast-model": "Set the fast model name to use for generation",
	}
}

func (m FastModelName) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	newValue = FastModelName(args[0])
	remainArgs = args[1:]
	return
}
