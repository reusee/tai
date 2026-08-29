package flags

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
)

// TheoryOfHandoffModelPrecedence documents how config values from multiple
// files select the handoff model. See pipeline.TheoryOfHandoffModel for the
// fallback chain and generator selection.
const TheoryOfHandoffModelPrecedence = `
HandoffModel is a scalar selection, never an accumulation: when several
config files set the same path, the first non-empty value wins. HandleConfig
receives values in loader root order, and the tai loader lists roots from
most local to most global (working directory, module root, user config dir,
/etc), so first-wins lets a project-level config pin the handoff model
unaffected by global settings; a more global root applies only when no more
local file sets the path, and an explicitly empty string counts as unset.
The value is a single string — lists are rejected, matching the tai schema.
Precedence layers compose: app-specific paths (e.g. cmd_ai.handoff_model)
come after the generic paths in ConfigPathsFunc and override them
(configs.TheoryOfConfigPathPrecedence), and the -handoff-model flag
overrides every config value because flags.Parse runs after configs.Load
(TheoryOfConfigFlagParity).
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
