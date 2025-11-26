package tailang

type While struct{}

var _ Function = While{}

func (w While) FunctionName() string {
	return "while"
}

func (w While) Call(env *Env, stream TokenStream) (any, error) {
	condBlock, err := ParseBlock(stream)
	if err != nil {
		return nil, err
	}

	bodyBlock, err := ParseBlock(stream)
	if err != nil {
		return nil, err
	}

	var lastRes any
	for {
		condRes, err := env.Evaluate(NewSliceTokenStream(condBlock))
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

		lastRes, err = env.Evaluate(NewSliceTokenStream(bodyBlock))
		if err != nil {
			return nil, err
		}
	}

	return lastRes, nil
}
