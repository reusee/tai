package tailang

import (
	"fmt"
	"reflect"
)

type Def struct {
	Type reflect.Type `tai:"type"`
}

var _ Function = Def{}

func (d Def) FunctionName() string {
	return "def"
}

func (d Def) Call(env *Env, stream TokenStream) (any, error) {
	// Name
	tok, err := stream.Current()
	if err != nil {
		return nil, err
	}
	if tok.Kind != TokenIdentifier && tok.Kind != TokenUnquotedString {
		return nil, fmt.Errorf("expected identifier")
	}
	name := tok.Text
	stream.Consume()

	if IsKeyword(name) {
		return nil, fmt.Errorf("cannot define keyword: %s", name)
	}

	if _, ok := env.Vars[name]; ok {
		return nil, fmt.Errorf("variable %s already defined", name)
	}

	// Value
	value, err := env.evalExpr(stream, d.Type)
	if err != nil {
		return nil, err
	}

	if d.Type != nil {
		if value == nil {
			switch d.Type.Kind() {
			case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
				// OK
			default:
				return nil, fmt.Errorf("cannot assign nil to %v", d.Type)
			}
		} else {
			valV := reflect.ValueOf(value)
			valV = convertType(valV, d.Type)
			if !valV.Type().AssignableTo(d.Type) {
				return nil, fmt.Errorf("cannot assign %v to %v", valV.Type(), d.Type)
			}
			value = valV.Interface()
		}
	}
	env.Define(name, value)
	return value, nil
}
