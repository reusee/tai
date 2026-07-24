package codes

import "github.com/reusee/tai/flags"

const TheoryOfPlan = `
The plan mechanism is opt-in via the -plan flag. When enabled, the system prompt
includes the MandatoryPlanningSystemPrompt, which requires every task to begin
with an overall plan and task decomposition, emitted as a plan-only first round,
followed by execution rounds delimited by continue blocks. When disabled (the
default), the planning mandate is omitted from the system prompt, and the model
may complete tasks in a single response without a continue block. This makes
planning a user-controlled trade-off: enabling it adds output-length safety for
large or complex tasks at the cost of an extra round-trip per task, while
disabling it allows faster turnaround for simple tasks.
`

// Plan controls whether the mandatory planning mechanism is enabled.
// When true, the system prompt includes MandatoryPlanningSystemPrompt,
// requiring every task to begin with a plan-only first round followed by
// execution rounds delimited by continue blocks.
// When false (the default), the planning mandate is omitted and the model
// may complete tasks in a single response without a continue block.
// See TheoryOfPlan.
type Plan bool

func (Module) Plan() Plan {
	return false
}

var _ flags.Flag = Plan(true)

func (p Plan) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return Plan(true), args, nil
}

func (p Plan) Keys() map[string]string {
	return map[string]string{
		"-plan": "Enable mandatory planning and multi-round generation",
	}
}
