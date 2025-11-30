package tailang

import (
	"fmt"
	"reflect"
)

type Repeat struct{}

var _ Function = Repeat{}

func (r Repeat) FunctionName() string {
	return "repeat"
}

func (r Repeat) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	tok, err := stream.Current()
	if err != nil {
		return nil, err
	}
	if tok.Kind != TokenIdentifier && tok.Kind != TokenUnquotedString {
		return nil, fmt.Errorf("expected identifier")
	}
	varName := tok.Text
	stream.Consume()

	countVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	count, ok := AsInt(countVal)
	if !ok {
		return nil, fmt.Errorf("repeat expects integer count")
	}

	bodyVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	body, ok := bodyVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for repeat body, got %T", bodyVal)
	}

	var lastRes any
	for i := 1; i <= count; i++ {
		loopEnv := &Env{
			Parent: env,
			Vars:   make(map[string]any),
		}
		loopEnv.Define(varName, i)

		lastRes, err = loopEnv.Evaluate(NewSliceTokenStream(body.Body))
		if err != nil {
			if _, ok := err.(BreakSignal); ok {
				break
			}
			if _, ok := err.(ContinueSignal); ok {
				continue
			}
			return nil, err
		}
	}
	return lastRes, nil
}
