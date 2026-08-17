package flags

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
)

// HandoffModels is the list of models used for handoff in order. Each retry
// attempt in the handoff process uses the next model in the list, cycling
// back to the beginning when the list is exhausted. When empty, the fast
// model (FastModelName) is used if configured; otherwise the default model
// (ModelName) is used. See states.TheoryOfHandoffModel.
type HandoffModels []string

func (Module) HandoffModels() (ret HandoffModels) {
	return
}

var _ Flag = HandoffModels(nil)

func (m HandoffModels) Keys() map[string]string {
	return map[string]string{
		"-handoff-model": "Set a handoff model to use for generation (repeatable, tried in order)",
	}
}

func (m HandoffModels) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := append(slices.Clone(m), args[0])
	return &ret, args[1:], nil
}

var _ configs.DynamicPathsConfig = HandoffModels(nil)

func (m HandoffModels) ConfigPaths() []string {
	return nil
}

func (m HandoffModels) ConfigPathsFunc() any {
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

func (m HandoffModels) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := slices.Clone(m)
	for _, v := range values {
		switch v.Kind() {
		case cue.StringKind:
			var s string
			if err := v.Decode(&s); err != nil {
				return nil, err
			}
			if s != "" {
				ret = append(ret, s)
			}
		case cue.ListKind:
			var list []string
			if err := v.Decode(&list); err != nil {
				return nil, err
			}
			ret = append(ret, list...)
		}
	}
	return &ret, nil
}
