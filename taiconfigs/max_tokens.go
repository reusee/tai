package taiconfigs

import (
	"math"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/vars"
)

type MaxTokens int

var _ configs.Configurable = MaxTokens(0)

func (m MaxTokens) ConfigExpr() string {
	return "MaxTokens"
}

var maxTokensFlag = cmds.Var[int]("-max-tokens")

func (Module) MaxTokens(
	loader configs.Loader,
) MaxTokens {
	maxTokens := math.MaxInt

	// flag
	if *maxTokensFlag != 0 {
		maxTokens = min(maxTokens, *maxTokensFlag)
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
