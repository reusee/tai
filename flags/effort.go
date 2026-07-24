package flags

import "fmt"

type Effort string

func (Module) Effort() (ret Effort) {
	return
}

var _ Flag = Effort("")

func (e Effort) Keys() map[string]string {
	return map[string]string{
		"-effort": "Set the reasoning effort level (e.g. low, medium, high)",
	}
}

func (e Effort) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	newValue = Effort(args[0])
	remainArgs = args[1:]
	return
}
