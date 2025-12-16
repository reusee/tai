package tailang

import (
	"fmt"
	"reflect"
)

type UserFunc struct {
	FuncName      string
	Params        []string
	Body          *Block
	DefinitionEnv *Env
}

var _ Function = UserFunc{}

func (u UserFunc) FunctionName() string {
	return u.FuncName
}

func (u UserFunc) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	args := make([]any, 0, len(u.Params))

	for i := 0; i < len(u.Params); i++ {
		arg, err := env.evalExpr(stream, nil)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i, err)
		}
		args = append(args, arg)
	}

	return u.CallArgs(args)
}

func (u UserFunc) CallArgs(args []any) (any, error) {
	if len(args) != len(u.Params) {
		return nil, fmt.Errorf("argument count mismatch: expected %d, got %d", len(u.Params), len(args))
	}

	callEnv := &Env{
		Parent:      u.DefinitionEnv,
		Vars:        make(map[string]any),
		IsFuncFrame: true,
	}

	defer func() {
		for i := len(callEnv.Defers) - 1; i >= 0; i-- {
			callEnv.Defers[i]()
		}
	}()

	for i, param := range u.Params {
		callEnv.Define(param, args[i])
	}

	bodyStream := NewSliceTokenStream(u.Body.Body)
	res, err := callEnv.Evaluate(bodyStream)
	if err != nil {
		if ret, ok := err.(ReturnSignal); ok {
			return ret.Val, nil
		}
		return nil, err
	}
	return res, nil
}
