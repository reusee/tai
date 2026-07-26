package flags

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
)

type FastModelName string

func (Module) FastModelName() (ret FastModelName) {
	return
}

var _ Flag = FastModelName("")

func (m FastModelName) Keys() map[string]string {
	return map[string]string{
		"-fast-model": "Set the fast model name to use for generation",
	}
}

func (m FastModelName) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := FastModelName(args[0])
	return &ret, args[1:], nil
}

var _ configs.DynamicPathsConfig = FastModelName("")

func (m FastModelName) ConfigPaths() []string {
	return nil
}

func (m FastModelName) ConfigPathsFunc() any {
	return func(
		appName apps.Name,
	) []string {
		if appName != "" {
			return []string{
				"fast_model_name",
				"fast_model",
				string(appName) + ".fast_model_name",
				string(appName) + ".fast_model",
			}
		}
		return []string{
			"fast_model_name",
			"fast_model",
		}
	}
}

func (m FastModelName) HandleConfig(path string, values []*cue.Value) (any, error) {
	if err := values[0].Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}
