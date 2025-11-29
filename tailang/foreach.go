package tailang

import (
	"fmt"
	"reflect"
)

type Foreach struct{}

var _ Function = Foreach{}

func (f Foreach) FunctionName() string {
	return "foreach"
}

func (f Foreach) Call(env *Env, stream TokenStream) (any, error) {
	tok, err := stream.Current()
	if err != nil {
		return nil, err
	}
	if tok.Kind != TokenIdentifier && tok.Kind != TokenUnquotedString {
		return nil, fmt.Errorf("expected identifier")
	}
	varName := tok.Text
	stream.Consume()

	listVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	if listVal == nil {
		return nil, fmt.Errorf("foreach expects a list, got nil")
	}

	bodyVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	body, ok := bodyVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for foreach body, got %T", bodyVal)
	}

	vList := reflect.ValueOf(listVal)
	switch vList.Kind() {
	case reflect.Slice, reflect.Array:
		var lastRes any
		for i := 0; i < vList.Len(); i++ {
			elem := vList.Index(i).Interface()

			loopEnv := &Env{
				Parent: env,
				Vars:   make(map[string]any),
			}
			loopEnv.Define(varName, elem)

			lastRes, err = loopEnv.Evaluate(NewSliceTokenStream(body.Body))
			if err != nil {
				return nil, err
			}
		}
		return lastRes, nil
	default:
		return nil, fmt.Errorf("foreach expects a list, got %T", listVal)
	}
}
