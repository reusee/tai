package flags

import (
	"fmt"
	"math"
	"strconv"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

type MaxTokens int

func (Module) MaxTokens() MaxTokens {
	return math.MaxInt
}

var _ configs.Config = MaxTokens(0)

func (m MaxTokens) ConfigPaths() []string {
	return []string{"max_tokens", "max_context_tokens"}
}

func (m MaxTokens) HandleConfig(path string, values []*cue.Value) (any, error) {
	var n MaxTokens
	if err := values[0].Decode(&n); err != nil {
		return nil, err
	}
	return n, nil
}

var _ Flag = MaxTokens(0)

func (m MaxTokens) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting int, got empty")
	}
	n, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, nil, err
	}
	ret := MaxTokens(n)
	return &ret, args[1:], nil
}

func (m MaxTokens) Keys() map[string]string {
	return map[string]string{
		"-max-tokens": "Set the maximum token budget for context",
	}
}
