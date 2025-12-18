package tailang

import (
	"fmt"
	"reflect"
)

type While struct{}

var _ Function = While{}

func (w While) FunctionName() string {
	return "while"
}

func (w While) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	recorder := &recordingStream{
		stream: stream,
	}

	condVal, err := env.evalExpr(recorder, nil)
	if err != nil {
		return nil, err
	}

	bodyBlockVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	bodyBlock, ok := bodyBlockVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for while body, got %T", bodyBlockVal)
	}

	var lastRes any
	for {
		isTrue := false
		if b, ok := condVal.(bool); ok {
			isTrue = b
		} else {
			isTrue = condVal != nil && condVal != false
		}

		if !isTrue {
			break
		}

		lastRes, err = env.NewScope().Evaluate(NewSliceTokenStream(bodyBlock.Body))
		if err != nil {
			if _, ok := err.(BreakSignal); ok {
				break
			}
			if _, ok := err.(ContinueSignal); ok {
				goto EvaluateCond
			}
			return nil, err
		}

	EvaluateCond:
		condVal, err = env.Evaluate(NewSliceTokenStream(recorder.tokens))
		if err != nil {
			return nil, err
		}
	}

	return lastRes, nil
}
