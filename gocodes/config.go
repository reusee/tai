package gocodes

import (
	"fmt"
	"strconv"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

// Debug configs.Config implementation for the gocodes module.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Debug(false)

func (d Debug) ConfigPaths() []string {
	return []string{"go.debug"}
}

func (d Debug) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return Debug(b), nil
}

// IncludeStdLib configs.Config implementation.

var _ configs.Config = IncludeStdLib(false)

func (i IncludeStdLib) ConfigPaths() []string {
	return []string{"go.include_std"}
}

func (i IncludeStdLib) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return IncludeStdLib(b), nil
}

// ShowTokenCounts configs.Config implementation.

var _ configs.Config = ShowTokenCounts(false)

func (s ShowTokenCounts) ConfigPaths() []string {
	return []string{"go.show_token_counts"}
}

func (s ShowTokenCounts) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return ShowTokenCounts(b), nil
}

// MaxPackageDistanceFromRoot flags.Flag implementation. The configs.Config
// implementation is in debug.go. See flags.TheoryOfConfigFlagParity.

var _ flags.Flag = MaxPackageDistanceFromRoot(0)

func (m MaxPackageDistanceFromRoot) Keys() map[string]string {
	return map[string]string{
		"-max-distance": "Set the maximum import distance from root packages",
	}
}

func (m MaxPackageDistanceFromRoot) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting int, got empty")
	}
	n, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, nil, err
	}
	return MaxPackageDistanceFromRoot(n), args[1:], nil
}
