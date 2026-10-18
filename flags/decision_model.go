package flags

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
)

// TheoryOfDecisionModelPrecedence documents how config values from multiple
// files select the decision model: a scalar selection, never an accumulation.
// The decision model is consumed by the codes pipeline's task decision and
// is not consumed by the review process.
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

The decision model is consumed by the codes pipeline's task decision: it
judges whether the session's task is a programming task, which decides
whether the codes base prompt enters the system prompt (see
pipeline.TheoryOfTaskDecision). It is not consumed by the review process:
the goal runner's post-done verification loops and the post-run review
sessions run on the review-model rotation (see
pipeline.TheoryOfGoalReviewModel).
`

// DecisionModel is the model that judges decision tasks. The codes pipeline's
// task decision consumes it: it judges whether the session's task is a
// programming task, which decides whether the codes base prompt enters the
// system prompt (see pipeline.TheoryOfTaskDecision). It is not consumed by
// the review process: the goal runner's post-done verification loops and the
// post-run review sessions run on the review-model rotation (see
// pipeline.TheoryOfGoalReviewModel). The value is selected by the
// -decision-model flag or the decision_model config path. See
// TheoryOfDecisionModelPrecedence.
type DecisionModel string

func (Module) DecisionModel() (ret DecisionModel) {
	return
}

var _ Flag = DecisionModel("")

func (m DecisionModel) Keys() map[string]string {
	return map[string]string{
		"-decision-model": "Set the model that judges whether the session's task is a programming task",
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
