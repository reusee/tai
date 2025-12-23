package codes

import (
	"context"
	"slices"
	"strings"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/phases"
)

type ActionDo struct {
	ActionArgument   dscope.Inject[ActionArgument]
	DiffHandler      dscope.Inject[codetypes.DiffHandler]
	BuildGenerate    dscope.Inject[phases.BuildGenerate]
	BuildChat        dscope.Inject[phases.BuildChat]
	GetPlanGenerator dscope.Inject[GetPlanGenerator]
	GetCodeGenerator dscope.Inject[GetCodeGenerator]
	Logger           dscope.Inject[logs.Logger]
	Tap              dscope.Inject[debugs.Tap]
}

var _ Action = ActionDo{}

func (Module) ActionDo(
	inject dscope.InjectStruct,
	diffHandler codetypes.DiffHandler,
) (ret ActionDo) {
	inject(&ret)
	return
}

func (a ActionDo) InitialPhase(cont phases.Phase) phases.Phase {
	return a.plan(cont)
}

func (a ActionDo) plan(cont phases.Phase) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		state0 := state

		state, err := state.AppendContent(&generators.Content{
			Role: "user",
			Parts: []generators.Part{
				generators.Text(`The primary goal is: ` + string(a.ActionArgument()) + `

You are currently in the **PLANNING PHASE**.
Your task is to create a detailed execution plan.
**ABSOLUTELY NO CODE IMPLEMENTATION IS ALLOWED.**

Instructions:
1. **Analyze** the request and the provided code.
2. **Design** the solution, explaining your strategy and architectural choices in plain language.
3. **Plan** the step-by-step actions required to achieve the goal.

**Constraints:**
- **DO NOT** write any code blocks (no ` + "`" + "```" + "`" + `).
- **DO NOT** write function bodies or implementations.
- **DO NOT** provide diffs.
- **ONLY** use natural language to describe the changes.

If your response contains code blocks or implementations, it will be rejected.
Focus on *what* needs to be done and *why*, not *how* to code it yet.
`),
			},
		})
		if err != nil {
			return nil, nil, err
		}

		generator, err := a.GetPlanGenerator()()
		if err != nil {
			return nil, nil, err
		}
		return a.BuildGenerate()(generator, nil)(
			a.checkPlan(state0, cont),
		), state, nil
	}
}

func (a ActionDo) checkPlan(planState generators.State, cont phases.Phase) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {

		contents := state.Contents()

		// check finish reason
		for _, content := range slices.Backward(contents) {
			for _, part := range content.Parts {
				if f, ok := part.(generators.FinishReason); ok {
					if !strings.EqualFold(string(f), "stop") {
						// unexpected finish reason, retry plan
						return a.plan(cont), planState, nil
					}
				}
			}
		}

		// check output
		hasOutput := false
		var text generators.Text
		for _, content := range contents {
			if content.Role != generators.RoleModel && content.Role != generators.RoleAssistant {
				continue
			}
			for _, part := range content.Parts {
				switch part := part.(type) {
				case generators.Text:
					hasOutput = hasOutput || len(part) > 0
					text += part
				case generators.Thought:
					hasOutput = hasOutput || len(part) > 0
				case generators.FuncCall:
					hasOutput = true
				}
			}
		}
		if !hasOutput {
			// no output, retry plan
			return a.plan(cont), planState, nil
		}
		strText := string(text)
		if strings.Contains(strText, "[[[ MODIFY") ||
			strings.Contains(strText, "[[[ ADD_BEFORE") ||
			strings.Contains(strText, "[[[ ADD_AFTER") ||
			strings.Contains(strText, "[[[ DELETE") {
			// has code, skip do phase
			return cont, state, nil
		}

		return a.do(cont), state, nil
	}
}

func (a ActionDo) do(cont phases.Phase) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		codeGenerator, err := a.GetCodeGenerator()()
		if err != nil {
			return nil, nil, err
		}
		state, err = state.AppendContent(&generators.Content{
			Role: "user",
			Parts: []generators.Part{
				generators.Text(`Now, based on the plan from the previous step, execute all remaining steps to achieve the goal. Review the conversation history to understand what has been accomplished so far and implement all uncompleted tasks from the plan. Provide all necessary code modifications to complete as many steps as possible in this single response.

If the remaining tasks involve writing or changing code, provide all necessary code modifications. If they involve analysis, providing information, or asking questions, do that instead.

**IMPORTANT**: Focus exclusively on the tasks outlined in your plan and the original goal. Do not make any changes to code that are not directly related to achieving this goal, even if you identify other potential improvements or errors.

If the goal is fully achieved, state "Goal achieved." and then you may propose further optimizations or enhancements relevant to the original objective.

Always include a clear rationale for your decisions and the anticipated impact of the steps.
` + a.DiffHandler().RestatePrompt()),
			},
		})
		if err != nil {
			return nil, nil, err
		}
		return a.BuildGenerate()(codeGenerator, nil)(
			a.BuildChat()(codeGenerator, nil)(
				cont,
			),
		), state, nil
	}
}

func (a ActionDo) Name() string {
	return "do"
}

func (a ActionDo) DefineCmds() {
	cmds.Define(a.Name(), cmds.Func(func(input string) {
		actionNameFlag = a.Name()
		actionArgumentFlag = ActionArgument(input)
	}).Desc("do something with planning"))

	// pre-defined goals

	cmds.Define("bugs", cmds.Func(func() {
		actionNameFlag = a.Name()
		prompt := `
Your goal is to improve code quality and correctness by identifying and fixing the most critical defects. Analyze the code to identify bugs, potential issues, and security vulnerabilities. From the issues you find, select the one or two most severe ones to address.

**CRITICAL REQUIREMENT:** For every bug you identify, you **must** write a test case that reproduces the bug and proves its existence. The test case is not optional; it is the primary method of validation. If a suspected issue cannot be exposed by a failing test, it is not considered a valid bug for this task.

Your process for each issue should be:
1. Identify the potential bug.
2. Create a test case that fails due to this bug (reproduction).
3. Fix the bug so the test passes.

Focus only on fixing significant problems, not on stylistic improvements or minor optimizations unless they address a specific defect. If no issues are found that can be reproduced with a test, state "No issues found" without fabricating problems.
`
		actionArgumentFlag = ActionArgument(prompt)
	}).Desc("find bugs and fix"))
	cmds.Define("-focus", cmds.Func(func(arg string) {
		if arg != "" {
			actionArgumentFlag += " Pay special attention to " + ActionArgument(arg) + "."
		}
	}).Desc("set focus"))

	cmds.Define("next", cmds.Func(func() {
		actionNameFlag = a.Name()
		actionArgumentFlag = `理解最终目标和当前进展，确定下一步行动，然后提供行动的帮助。优先注意 @@ai 标记，然后注意 TODO 标记。`
	}).Desc("find the best next step and finish it"))

}

func (a ActionDo) InitialGenerator() (generators.Generator, error) {
	return a.GetPlanGenerator()()
}
