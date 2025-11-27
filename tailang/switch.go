package tailang

import "fmt"

type Switch struct{}

var _ Function = Switch{}

func (s Switch) FunctionName() string {
	return "switch"
}

func (s Switch) Call(env *Env, stream TokenStream) (any, error) {
	val, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}

	bodyVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	bodyBlock, ok := bodyVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for switch body, got %T", bodyVal)
	}

	bodyStream := NewSliceTokenStream(bodyBlock.Body)

	for {
		tok, err := bodyStream.Current()
		if err != nil || tok.Kind == TokenEOF {
			break
		}

		caseVal, err := env.evalExpr(bodyStream, nil)
		if err != nil {
			return nil, err
		}

		blockVal, err := env.evalExpr(bodyStream, nil)
		if err != nil {
			return nil, err
		}
		block, ok := blockVal.(*Block)
		if !ok {
			return nil, fmt.Errorf("expected block for case body, got %T", blockVal)
		}

		if Eq(val, caseVal) {
			return env.Evaluate(NewSliceTokenStream(block.Body))
		}
	}
	return nil, nil
}
