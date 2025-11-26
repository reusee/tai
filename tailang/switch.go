package tailang

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

	bodyTokens, err := ParseBlock(stream)
	if err != nil {
		return nil, err
	}

	bodyStream := NewSliceTokenStream(bodyTokens)

	for {
		tok, err := bodyStream.Current()
		if err != nil || tok.Kind == TokenEOF {
			break
		}

		caseVal, err := env.evalExpr(bodyStream, nil)
		if err != nil {
			return nil, err
		}

		block, err := ParseBlock(bodyStream)
		if err != nil {
			return nil, err
		}

		if Eq(val, caseVal) {
			return env.Evaluate(NewSliceTokenStream(block))
		}
	}
	return nil, nil
}
