package codes

import (
	"context"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/phases"
)

type ActionChat struct {
	BuildChat           dscope.Inject[phases.BuildChat]
	BuildGenerate       dscope.Inject[phases.BuildGenerate]
	ActionArgument      dscope.Inject[ActionArgument]
	GetDefaultGenerator dscope.Inject[GetDefaultGenerator]
}

var _ Action = ActionChat{}

func (Module) ActionChat(
	inject dscope.InjectStruct,
) (ret ActionChat) {
	inject(&ret)
	return
}

func (a ActionChat) InitialPhase(cont phases.Phase) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		generator, err := a.GetDefaultGenerator()()
		if err != nil {
			return nil, nil, err
		}

		if arg := a.ActionArgument(); arg != "" {
			state, err = state.AppendContent(&generators.Content{
				Role: "user",
				Parts: []generators.Part{
					generators.Text(string(arg)),
				},
			})
			if err != nil {
				return nil, nil, err
			}
			return a.BuildGenerate()(
				generator,
				a.BuildChat()(
					generator,
					cont,
				),
			), state, nil
		}

		return a.BuildChat()(generator, cont), state, nil
	}
}

func (a ActionChat) Name() string {
	return "chat"
}

func (a ActionChat) DefineCmds() {
	cmds.Define(a.Name(), cmds.Func(func(args *string) {
		actionNameFlag = a.Name()
		actionArgumentFlag = ActionArgument(*args)
	}).Desc("chat interactively"))
}

func (a ActionChat) InitialGenerator() (generators.Generator, error) {
	return a.GetDefaultGenerator()()
}
