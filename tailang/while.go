package tailang

import "fmt"

type While struct{}

var _ Function = While{}

func (w While) FunctionName() string {
	return "while"
}

func (w While) Call(env *Env, stream TokenStream) (any, error) {
	condBlockVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	condBlock, ok := condBlockVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for while condition, got %T", condBlockVal)
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
		condRes, err := env.Evaluate(NewSliceTokenStream(condBlock.Body))
		if err != nil {
			return nil, err
		}

		isTrue := false
		if b, ok := condRes.(bool); ok {
			isTrue = b
		} else {
			isTrue = condRes != nil && condRes != false
		}

		if !isTrue {
			break
		}

		lastRes, err = env.Evaluate(NewSliceTokenStream(bodyBlock.Body))
		if err != nil {
			return nil, err
		}
	}

	return lastRes, nil
}
