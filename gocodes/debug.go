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

type Debug bool

func (Module) Debug() Debug {
	return false
}

var _ flags.Flag = Debug(false)

func (d Debug) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	ret := Debug(true)
	return &ret, args, nil
}

func (d Debug) Keys() map[string]string {
	return map[string]string{
		"-debug-gocodes": "Enable debug logging for the gocodes module",
	}
}

func (d Debug) ConfigPaths() []string {
	return []string{"go.debug"}
}

func (d Debug) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := Debug(b)
	return &ret, nil
}

// MaxPackageDistanceFromRoot configs.Config and flags.Flag implementation.
// The type is declared in files.go.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = MaxPackageDistanceFromRoot(0)

func (m MaxPackageDistanceFromRoot) ConfigPaths() []string {
	return []string{"go.max_distance"}
}

func (m MaxPackageDistanceFromRoot) HandleConfig(path string, values []*cue.Value) (any, error) {
	var n int
	if err := values[0].Decode(&n); err != nil {
		return nil, err
	}
	ret := MaxPackageDistanceFromRoot(n)
	return &ret, nil
}

var _ flags.Flag = MaxPackageDistanceFromRoot(0)

func (m MaxPackageDistanceFromRoot) Keys() map[string]string {
	return map[string]string{
		"-max-distance": "Set the maximum import distance from root packages",
	}
}

func (m MaxPackageDistanceFromRoot) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting int, got empty")
	}
	n, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, nil, err
	}
	ret := MaxPackageDistanceFromRoot(n)
	return &ret, args[1:], nil
}
