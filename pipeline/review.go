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
var _ configs.Config = Review(false)
var _ flags.Flag = Review(true)

// Review controls whether a review loop runs after the main generation
// loop (or after the goal command completes). When enabled, the changes
// made during generation are reviewed and corrected by one or more
// independent models. See TheoryOfReviewLoop.
type Review bool

// ReviewModels lists the models used for review, in order. Each model runs
// a separate review generation session with a fresh scope. When empty,
// the model selected by the -model flag is reused: the resolved generator's
// Spec is not reusable because built-in shortcuts (flash, gemini, ...) and
// the ollama shorthand do not set Spec.Name, and their Spec.Model values are
// not resolvable model names. See TheoryOfReviewLoop.
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
		"-review-model": "Set a review model to use for the review loop (repeatable, run in order)",
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

func (r Review) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := Review(true)
	return &ret, args, nil
}

func (r Review) Keys() map[string]string {
	return map[string]string{
		"-review": "Run a review loop after generation to review and fix changes",
	}
}

func (r Review) ConfigPaths() []string {
	return []string{"review"}
}

func (r Review) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := Review(b)
	return &ret, nil
}

func (Module) ReviewModels() ReviewModels {
	return nil
}

func (Module) Review() Review {
	return false
}
