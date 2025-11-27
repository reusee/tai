package tailang

import "fmt"

type Switch struct{}

type DefaultCase struct{}

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

	var cases []any

	for {
		tok, err := bodyStream.Current()
		if err != nil || tok.Kind == TokenEOF {
			break
		}

		if tok.Kind == TokenIdentifier && tok.Text == "default" {
			bodyStream.Consume()
			cases = append(cases, DefaultCase{})
			continue
		}

		exprVal, err := env.evalExpr(bodyStream, nil)
		if err != nil {
			return nil, err
		}

		if block, ok := exprVal.(*Block); ok {
			matched := false
			for _, c := range cases {
				if _, isDef := c.(DefaultCase); isDef {
					matched = true
					break
				}
				if Eq(val, c) {
					matched = true
					break
				}
			}

			if matched {
				return env.NewScope().Evaluate(NewSliceTokenStream(block.Body))
			}
			cases = nil
		} else {
			cases = append(cases, exprVal)
		}
	}
	return nil, nil
}
