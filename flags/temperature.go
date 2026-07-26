package flags

import (
	"fmt"
	"strconv"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

// TemperatureFlag wraps an optional float32 for the generation temperature.
// A nil Value means the flag was never set; a non-nil Value points to the
// resolved temperature. It can be configured via the -temperature flag or
// the "temperature" config path.
type TemperatureFlag struct {
	Value *float32
}

func (Module) TemperatureFlag() (ret TemperatureFlag) {
	return
}

var _ Flag = TemperatureFlag{}

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

// configs.Config implementation. See TheoryOfConfigFlagParity.

var _ configs.Config = TemperatureFlag{}

func (t TemperatureFlag) ConfigPaths() []string {
	return []string{"temperature"}
}

func (t TemperatureFlag) HandleConfig(path string, values []*cue.Value) (any, error) {
	var f float32
	if err := values[0].Decode(&f); err != nil {
		return nil, err
	}
	return TemperatureFlag{Value: &f}, nil
}
