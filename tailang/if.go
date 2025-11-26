package tailang

type If struct{}

var _ Function = If{}

func (i If) FunctionName() string {
	return "if"
}

func (i If) Call(env *Env, stream TokenStream) (any, error) {
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

	trueBlock, err := ParseBlock(stream)
	if err != nil {
		return nil, err
	}

	var falseBlock []*Token
	tok, err := stream.Current()
	if err == nil && tok.Kind == TokenIdentifier && tok.Text == "else" {
		stream.Consume()
		falseBlock, err = ParseBlock(stream)
		if err != nil {
			return nil, err
		}
	}

	if isTrue {
		return env.Evaluate(NewSliceTokenStream(trueBlock))
	} else if len(falseBlock) > 0 {
		return env.Evaluate(NewSliceTokenStream(falseBlock))
	}

	return nil, nil
}
