package gocodes

import (
	"os"
	"slices"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

type Envs []string

var _ configs.Config = Envs(nil)

func (e Envs) ConfigPaths() []string {
	return []string{"go_envs", "go.envs"}
}

func (e Envs) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := slices.Clone(e)
	for _, v := range values {
		var envs []string
		if err := v.Decode(&envs); err != nil {
			return nil, err
		}
		ret = append(ret, envs...)
	}
	return Envs(withModModEnv(ret)), nil
}

func (Module) Envs() (ret Envs) {
	ret = os.Environ()
	// Ensure GOFLAGS includes -mod=mod so go list can resolve packages without
	// requiring go.mod to be perfectly tidy. The -mod=mod flag allows go to
	// update go.mod automatically instead of erroring.
	// See TheoryOfModModEnv.
	ret = Envs(withModModEnv(ret))
	return
}
