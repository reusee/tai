package taiconfigs

import (
	"math"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/vars"
)

type MaxUserTokens int

var _ configs.Configurable = MaxUserTokens(0)

func (m MaxUserTokens) ConfigExpr() string {
	return "Go.MaxUserTokens"
}

var maxUserTokensFlag = cmds.Var[int]("-max-user-tokens")

func (Module) MaxUserTokens(
	loader configs.Loader,
) MaxUserTokens {
	maxTokens := math.MaxInt

	// flag
	if *maxUserTokensFlag != 0 {
		maxTokens = min(maxTokens, *maxUserTokensFlag)
	}

	// config
	if n := vars.FirstNonZero(
		configs.First[int](loader, "max_user_tokens"),
	); n != 0 {
		maxTokens = min(maxTokens, n)
	}

	return MaxUserTokens(maxTokens)
}
