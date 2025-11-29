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

	var pipedVal any
	hasPipe := false
	pipeLast := false
	if ps, ok := stream.(*PipedStream); ok && ps.HasValue {
		hasPipe = true
		pipedVal = ps.Value
		pipeLast = ps.PipeLast
	}

	startIdx := 0
	if hasPipe && !pipeLast {
		if len(u.Params) > 0 {
			args = append(args, pipedVal)
			startIdx = 1
		}
	}

	for i := startIdx; i < len(u.Params); i++ {
		if hasPipe && pipeLast && i == len(u.Params)-1 {
			args = append(args, pipedVal)
			continue
		}

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
		Parent: u.DefinitionEnv,
		Vars:   make(map[string]any),
	}

	for i, param := range u.Params {
		callEnv.Define(param, args[i])
	}

	bodyStream := NewSliceTokenStream(u.Body.Body)
	res, err := callEnv.Evaluate(bodyStream)
	if err != nil {
		return nil, err
	}
	return res, nil
}
