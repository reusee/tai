package tailang

import (
	"fmt"
)

type UserFunc struct {
	FuncName      string
	Params        []string
	Body          []*Token
	DefinitionEnv *Env
}

var _ Function = UserFunc{}

func (u UserFunc) Name() string {
	return u.FuncName
}

func (u UserFunc) Call(env *Env, stream TokenStream) (any, error) {
	args := make([]any, 0, len(u.Params))
	for i := range u.Params {
		arg, err := env.evalExpr(stream, nil)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i, err)
		}
		args = append(args, arg)
	}

	callEnv := &Env{
		Parent: u.DefinitionEnv,
		Vars:   make(map[string]Value),
	}

	for i, param := range u.Params {
		callEnv.Define(param, args[i])
	}

	bodyStream := NewSliceTokenStream(u.Body)
	return callEnv.Evaluate(bodyStream)
}
