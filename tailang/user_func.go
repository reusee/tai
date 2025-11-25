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

func (u UserFunc) Call(args ...any) (any, error) {
	env := &Env{
		Parent: u.DefinitionEnv,
		Vars:   make(map[string]Value),
	}

	if len(args) != len(u.Params) {
		return nil, fmt.Errorf("arity mismatch: expected %d, got %d", len(u.Params), len(args))
	}

	for i, param := range u.Params {
		env.Define(param, args[i])
	}

	stream := NewSliceTokenStream(u.Body)
	return env.Evaluate(stream)
}
