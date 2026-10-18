package flags

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
)

// TheoryOfDecisionModelPrecedence documents how config values from multiple
// files select the decision model. See pipeline.TheoryOfGoalReviewModel for
// where the model runs and how it composes with the review-model rotation.
const TheoryOfDecisionModelPrecedence = `
DecisionModel is a scalar selection, never an accumulation: when several
config files set the same path, the first non-empty value wins. HandleConfig
receives values in loader root order, and the tai loader lists roots from
most local to most global (working directory, module root, user config dir,
/etc), so first-wins lets a project-level config pin the decision model
unaffected by global settings; a more global root applies only when no more
local file sets the path, and an explicitly empty string counts as unset.
The value is a single string — lists are rejected, matching the tai schema.
Precedence layers compose: app-specific paths (e.g. cmd_ai.decision_model)
come after the generic paths in ConfigPathsFunc and override them
(configs.TheoryOfConfigPathPrecedence), and the -decision-model flag
overrides every config value because flags.Parse runs after configs.Load
(TheoryOfConfigFlagParity).
`

// DecisionModel is the model used for the goal runner's decision loops: the
// verification loops that follow a done block and decide whether the declared
// goal is genuinely achieved. It takes precedence over the review-model
// rotation; when empty, the rotation drives those loops if configured, and
// the default model otherwise. See TheoryOfDecisionModelPrecedence and
// pipeline.TheoryOfGoalReviewModel.
type DecisionModel string

func (Module) DecisionModel() (ret DecisionModel) {
	return
}

var _ Flag = DecisionModel("")

func (m DecisionModel) Keys() map[string]string {
	return map[string]string{
		"-decision-model": "Set a model for the goal verification (decision) loops",
	}
}

func (m DecisionModel) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := DecisionModel(args[0])
	return &ret, args[1:], nil
}

var _ configs.DynamicPathsConfig = DecisionModel("")

func (m DecisionModel) ConfigPaths() []string {
	return nil
}

func (m DecisionModel) ConfigPathsFunc() any {
	return func(
		appName apps.Name,
	) []string {
		if appName != "" {
			return []string{
				"decision_model_name",
				"decision_model",
				string(appName) + ".decision_model_name",
				string(appName) + ".decision_model",
			}
		}
		return []string{
			"decision_model_name",
			"decision_model",
		}
	}
}

func (m DecisionModel) HandleConfig(path string, values []*cue.Value) (any, error) {
	// Values arrive in loader root order, most local root first, so the
	// first non-empty value wins: a project-level config overrides
	// personal and system defaults, and an explicitly empty string counts
	// as unset. The value is a single string, never a list.
	// See TheoryOfDecisionModelPrecedence.
	for _, v := range values {
		if v.Kind() != cue.StringKind {
			return nil, fmt.Errorf("decision model config %q: expecting string, got %v", path, v.Kind())
		}
		var s string
		if err := v.Decode(&s); err != nil {
			return nil, err
		}
		if s != "" {
			ret := DecisionModel(s)
			return &ret, nil
		}
	}
	return nil, nil
}
