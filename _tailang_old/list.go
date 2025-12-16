package tailang

import (
	"fmt"
	"reflect"
)

type List struct {
	Elem reflect.Type `tai:"elem"`
}

var _ Function = List{}

func (l List) FunctionName() string {
	return "["
}

func (l List) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	var sliceType reflect.Type
	var elemType reflect.Type
	if l.Elem != nil {
		elemType = l.Elem
		sliceType = reflect.SliceOf(elemType)
	} else if expectedType != nil && expectedType.Kind() == reflect.Slice {
		sliceType = expectedType
		elemType = expectedType.Elem()
	} else {
		sliceType = reflect.TypeOf([]any{})
		elemType = reflect.TypeOf((*any)(nil)).Elem()
	}

	res := reflect.MakeSlice(sliceType, 0, 0)

	for {
		tok, err := stream.Current()
		if err != nil {
			return nil, err
		}
		if tok.Kind == TokenEOF {
			return nil, fmt.Errorf("unexpected EOF")
		}
		if tok.Kind == TokenSymbol && tok.Text == "]" {
			stream.Consume()
			break
		}

		val, err := env.evalExpr(stream, elemType)
		if err != nil {
			return nil, err
		}

		vVal, err := PrepareAssign(val, elemType)
		if err != nil {
			return nil, err
		}
		res = reflect.Append(res, vVal)
	}

	return res.Interface(), nil
}
