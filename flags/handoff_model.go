package flags

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
)

// HandoffModel is the model used for handoff. When empty, the fast model
// (FastModelName) is used if configured; otherwise the default model
// (ModelName) is used. See states.TheoryOfHandoffModel.
type HandoffModel string

func (Module) HandoffModel() (ret HandoffModel) {
	return
}

var _ Flag = HandoffModel("")

func (m HandoffModel) Keys() map[string]string {
	return map[string]string{
		"-handoff-model": "Set a handoff model to use for generation",
	}
}

func (m HandoffModel) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := HandoffModel(args[0])
	return &ret, args[1:], nil
}

var _ configs.DynamicPathsConfig = HandoffModel("")

func (m HandoffModel) ConfigPaths() []string {
	return nil
}

func (m HandoffModel) ConfigPathsFunc() any {
	return func(
		appName apps.Name,
	) []string {
		if appName != "" {
			return []string{
				"handoff_model_name",
				"handoff_model",
				string(appName) + ".handoff_model_name",
				string(appName) + ".handoff_model",
			}
		}
		return []string{
			"handoff_model_name",
			"handoff_model",
		}
	}
}

func (m HandoffModel) HandleConfig(path string, values []*cue.Value) (any, error) {
	var ret HandoffModel
	found := false
	for _, v := range values {
		switch v.Kind() {
		case cue.StringKind:
			var s string
			if err := v.Decode(&s); err != nil {
				return nil, err
			}
			if s != "" {
				ret = HandoffModel(s)
				found = true
			}
		case cue.ListKind:
			var list []string
			if err := v.Decode(&list); err != nil {
				return nil, err
			}
			for _, s := range list {
				if s != "" {
					ret = HandoffModel(s)
					found = true
					break
				}
			}
		}
	}
	if !found {
		return nil, nil
	}
	return &ret, nil
}
