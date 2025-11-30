package tailang

import "reflect"

type Defer struct{}

var _ Function = Defer{}

func (d Defer) FunctionName() string {
	return "defer"
}

func (d Defer) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	block, err := ParseBlock(stream)
	if err != nil {
		return nil, err
	}

	// Find the nearest function environment
	targetEnv := env
	for targetEnv != nil {
		if targetEnv.IsFuncFrame {
			break
		}
		targetEnv = targetEnv.Parent
	}
	if targetEnv == nil {
		// Fallback to current env if no function frame found (e.g. top level)
		targetEnv = env
	}

	targetEnv.Defers = append(targetEnv.Defers, func() {
		env.NewScope().Evaluate(NewSliceTokenStream(block.Body))
	})

	return nil, nil
}
