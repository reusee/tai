package taiconfigs

import (
	"fmt"
	"math"
	"strconv"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/vars"
)

type MaxTokens int

var _ configs.Configurable = MaxTokens(0)

func (m MaxTokens) TaigoConfigurable() {}

func (Module) MaxTokens(
	loader configs.Loader,
	maxTokensFlag MaxTokensFlag,
) MaxTokens {
	maxTokens := math.MaxInt

	// flag
	if maxTokensFlag.Value != nil {
		maxTokens = min(maxTokens, *maxTokensFlag.Value)
	}

	// config
	if n := vars.FirstNonZero(
		configs.First[int](loader, "max_context_tokens"),
		configs.First[int](loader, "max_tokens"),
	); n != 0 {
		maxTokens = min(maxTokens, n)
	}

	return MaxTokens(maxTokens)
}

type MaxTokensFlag struct {
	Value *int
}

func (Module) MaxTokensFlag() (ret MaxTokensFlag) {
	return
}

var _ flags.Flag = MaxTokensFlag{}

func (m MaxTokensFlag) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting int, got empty")
	}
	n, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, nil, err
	}
	newValue = MaxTokensFlag{
		Value: new(int(n)),
	}
	remainArgs = args[1:]
	return
}

func (m MaxTokensFlag) Keys() map[string]string {
	return map[string]string{
		"-max-tokens": "Set the maximum token budget for context",
	}
}
