package flags

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
)

type ModelName string

func (Module) ModelName() (ret ModelName) {
	return
}

var _ Flag = ModelName("")

func (m ModelName) Keys() map[string]string {
	return map[string]string{
		"-model": "Set the model name to use for generation",
	}
}

func (m ModelName) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := ModelName(args[0])
	return &ret, args[1:], nil
}

var _ configs.DynamicPathsConfig = ModelName("")

func (m ModelName) ConfigPaths() []string {
	return nil
}

func (m ModelName) ConfigPathsFunc() any {
	return func(
		appName apps.Name,
	) []string {
		if appName != "" {
			return []string{
				"model_name",
				"model",
				string(appName) + ".model_name",
				string(appName) + ".model",
			}
		}
		return []string{
			"model_name",
			"model",
		}
	}
}

func (m ModelName) HandleConfig(path string, values []*cue.Value) (any, error) {
	if err := values[0].Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}
