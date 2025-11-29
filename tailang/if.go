package tailang

import (
	"fmt"
	"reflect"
)

type If struct{}

var _ Function = If{}

func (i If) FunctionName() string {
	return "if"
}

func (i If) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	cond, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}

	isTrue := false
	if b, ok := cond.(bool); ok {
		isTrue = b
	} else {
		isTrue = cond != nil && cond != false
	}

	trueBlockVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	trueBlock, ok := trueBlockVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for if body, got %T", trueBlockVal)
	}

	var falseBlock *Block
	tok, err := stream.Current()
	if err == nil && tok.Kind == TokenIdentifier && tok.Text == "else" {
		stream.Consume()
		falseBlockVal, err := env.evalExpr(stream, nil)
		if err != nil {
			return nil, err
		}
		var ok bool
		falseBlock, ok = falseBlockVal.(*Block)
		if !ok {
			return nil, fmt.Errorf("expected block for else body, got %T", falseBlockVal)
		}
	}

	if isTrue {
		return env.NewScope().Evaluate(NewSliceTokenStream(trueBlock.Body))
	} else if falseBlock != nil {
		return env.NewScope().Evaluate(NewSliceTokenStream(falseBlock.Body))
	}

	return nil, nil
}
