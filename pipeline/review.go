package pipeline

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

var _ configs.Config = ReviewModels(nil)
var _ flags.Flag = ReviewModels(nil)

// ReviewModels lists the models the goal run's review phase runs on, in
// order: the first loop after a done declaration runs on the first model,
// each later review-phase loop takes the next model, and the selection
// wraps around to the first model after the last, so consecutive
// verification loops run on different models. When empty, post-done loops
// keep the default model. See TheoryOfGoalReviewModel.
type ReviewModels []string

func (m ReviewModels) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := append(slices.Clone(m), args[0])
	return &ret, args[1:], nil
}

func (m ReviewModels) Keys() map[string]string {
	return map[string]string{
		"-review-model": "Set the model a goal review-phase loop runs on (repeatable, run in rotation)",
	}
}

func (m ReviewModels) ConfigPaths() []string {
	return []string{"review_models"}
}

func (m ReviewModels) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := slices.Clone(m)
	for _, v := range values {
		var list []string
		if err := v.Decode(&list); err != nil {
			return nil, err
		}
		ret = append(ret, list...)
	}
	return &ret, nil
}

func (Module) ReviewModels() ReviewModels {
	return nil
}
