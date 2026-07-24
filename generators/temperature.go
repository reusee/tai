package generators

import (
	"fmt"
	"strconv"

	"github.com/reusee/tai/flags"
)

type TemperatureFlag struct {
	Value *float32
}

func (Module) TemperatureFlag() (ret TemperatureFlag) {
	return
}

var _ flags.Flag = TemperatureFlag{}

func (t TemperatureFlag) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting float, got empty")
	}
	num, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		return nil, nil, err
	}
	newValue = TemperatureFlag{
		Value: new(float32(num)),
	}
	remainArgs = args[1:]
	return
}

func (t TemperatureFlag) Keys() map[string]string {
	return map[string]string{
		"-temperature": "Set the generation temperature (0.0-2.0)",
	}
}
