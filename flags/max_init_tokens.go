package flags

import (
	"fmt"
	"math"
	"strconv"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

type MaxInitTokens int

func (Module) MaxInitTokens() MaxInitTokens {
	return math.MaxInt
}

var _ configs.Config = MaxInitTokens(0)

func (m MaxInitTokens) ConfigPaths() []string {
	return []string{"max_init_tokens"}
}

func (m MaxInitTokens) HandleConfig(path string, values []*cue.Value) (any, error) {
	return configs.DecodeConfig[MaxInitTokens](values)
}

var _ Flag = MaxInitTokens(0)

func (m MaxInitTokens) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting int, got empty")
	}
	n, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, nil, err
	}
	ret := MaxInitTokens(n)
	return &ret, args[1:], nil
}

func (m MaxInitTokens) Keys() map[string]string {
	return map[string]string{
		"-max-init-tokens": "Set the maximum token budget for initial input (a generator's own max_init_tokens overrides it)",
	}
}
