package gocodes

import (
	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

type Debug bool

func (Module) Debug() Debug {
	return false
}

var _ flags.Flag = Debug(false)

func (d Debug) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return Debug(true), args, nil
}

func (d Debug) Keys() map[string]string {
	return map[string]string{
		"-debug-gocodes": "Enable debug logging for the gocodes module",
	}
}

// MaxPackageDistanceFromRoot configs.Config implementation.
// Defined here because this file imports cuelang.org/go/cue.
// The type is declared in files.go.

var _ configs.Config = MaxPackageDistanceFromRoot(0)

func (m MaxPackageDistanceFromRoot) ConfigPaths() []string {
	return []string{"go.max_distance"}
}

func (m MaxPackageDistanceFromRoot) HandleConfig(path string, values []*cue.Value) (any, error) {
	var n int
	if err := values[0].Decode(&n); err != nil {
		return nil, err
	}
	return MaxPackageDistanceFromRoot(n), nil
}
