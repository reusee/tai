package flags

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
)

const TheoryOfHandoffModelPrecedence = `
HandoffModel is a scalar selection under the shared config precedence rule:
the first non-empty value wins, more local roots apply first, the precedence
layers compose, and the flag overrides every config value. The rule itself
lives in TheoryOfDecisionModelPrecedence. This type's generic config paths are
handoff_model_name and handoff_model, its app-specific path is
<app>.handoff_model, and its flag is -handoff-model.
`

// HandoffModel is the model used for handoff. When empty, the fast model
// (FastModelName) is used if configured; otherwise the default model
// (ModelName) is used. See pipeline.TheoryOfHandoffModel and
// TheoryOfHandoffModelPrecedence.
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
	// Values arrive in loader root order, most local root first, so the
	// first non-empty value wins: a project-level config overrides
	// personal and system defaults, and an explicitly empty string counts
	// as unset. The value is a single string, never a list.
	// See TheoryOfHandoffModelPrecedence.
	for _, v := range values {
		if v.Kind() != cue.StringKind {
			return nil, fmt.Errorf("handoff model config %q: expecting string, got %v", path, v.Kind())
		}
		var s string
		if err := v.Decode(&s); err != nil {
			return nil, err
		}
		if s != "" {
			ret := HandoffModel(s)
			return &ret, nil
		}
	}
	return nil, nil
}
