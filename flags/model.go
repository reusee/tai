package flags

import "fmt"

type ModelName string

func (Module) ModelName() (ret ModelName) {
	return
}

var _ Flag = ModelName("")

func (m ModelName) Keys() map[string]string {
	return map[string]string{
		"-model": "Set the model name to use for generation",
	}
}

func (m ModelName) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	newValue = ModelName(args[0])
	remainArgs = args[1:]
	return
}
