package tailang

import (
	"fmt"
	"reflect"
)

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

	var targetEnv *Env
	for e := env; e != nil; e = e.Parent {
		if e.IsFuncFrame {
			targetEnv = e
			break
		}
	}
	if targetEnv == nil {
		return nil, fmt.Errorf("defer must be inside a function")
	}

	targetEnv.Defers = append(targetEnv.Defers, func() {
		env.NewScope().Evaluate(NewSliceTokenStream(block.Body))
	})

	return nil, nil
}
