package tailang

import (
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

func (e *Env) Evaluate(tokenizer TokenStream) (any, error) {
	var result any
	for {
		t, err := tokenizer.Current()
		if err == io.EOF || t.Kind == TokenEOF {
			break
		}
		if err != nil {
			return nil, err
		}

		result, err = e.evalExpr(tokenizer, nil)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (e *Env) evalExpr(tokenizer TokenStream, expectedType reflect.Type) (any, error) {
	t, err := tokenizer.Current()
	if err != nil {
		return nil, err
	}

	if t.Kind == TokenString {
		tokenizer.Consume()
		return t.Text, nil
	}

	if t.Kind == TokenNumber {
		tokenizer.Consume()
		if strings.Contains(t.Text, ".") {
			return strconv.ParseFloat(t.Text, 64)
		}
		return strconv.Atoi(t.Text)
	}

	if t.Kind == TokenIdentifier || (t.Kind == TokenSymbol && t.Text == "[") {
		name := t.Text
		if name == "end" {
			return nil, fmt.Errorf("unexpected identifier 'end'")
		}
		if t.Kind == TokenSymbol && t.Text == "]" {
			return nil, fmt.Errorf("unexpected symbol ']'")
		}

		tokenizer.Consume()

		val, ok := e.Lookup(name)
		if !ok {
			if expectedType != nil && expectedType.Kind() == reflect.String {
				return name, nil
			}
			return nil, fmt.Errorf("undefined identifier: %s", name)
		}

		v := reflect.ValueOf(val)
		typ := v.Type()

		var callVal reflect.Value
		if typ.Kind() == reflect.Struct {
			ptr := reflect.New(typ)
			ptr.Elem().Set(v)
			callVal = ptr
		} else {
			callVal = v
		}

		// Named parameters
		for {
			next, err := tokenizer.Current()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if next.Kind != TokenNamedParam {
				break
			}

			paramName := strings.TrimPrefix(next.Text, ".")
			tokenizer.Consume()

			field := findField(callVal.Elem(), paramName)
			if !field.IsValid() {
				return nil, fmt.Errorf("unknown named parameter .%s for %s", paramName, name)
			}

			if field.Kind() == reflect.Bool {
				field.SetBool(true)
			} else {
				arg, err := e.evalExpr(tokenizer, field.Type())
				if err != nil {
					return nil, err
				}
				if err := setField(field, arg); err != nil {
					return nil, err
				}
			}
		}

		method := callVal.MethodByName("Call")
		if !method.IsValid() {
			if callVal.Kind() == reflect.Pointer {
				return callVal.Elem().Interface(), nil
			}
			return callVal.Interface(), nil
		}

		methodType := method.Type()
		numIn := methodType.NumIn()
		isVariadic := methodType.IsVariadic()
		args := make([]reflect.Value, 0, numIn)

		argOffset := 0
		if numIn > 0 && methodType.In(0) == reflect.TypeOf(e) {
			args = append(args, reflect.ValueOf(e))
			argOffset++
		}

		if numIn > argOffset && methodType.In(argOffset) == reflect.TypeOf((*TokenStream)(nil)).Elem() {
			args = append(args, reflect.ValueOf(tokenizer))
			results := method.Call(args)
			if len(results) == 0 {
				return nil, nil
			}
			last := results[len(results)-1]
			if last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
				if !last.IsNil() {
					return nil, last.Interface().(error)
				}
				if len(results) > 1 {
					return results[0].Interface(), nil
				}
				return nil, nil
			}
			return results[0].Interface(), nil
		}

		for i := argOffset; i < numIn; i++ {
			argType := methodType.In(i)

			if isVariadic && i == numIn-1 {
				elemType := argType.Elem()
				for {
					pt, err := tokenizer.Current()
					if err == io.EOF {
						break
					}
					if err != nil {
						return nil, err
					}

					if pt.Kind == TokenEOF {
						break
					}

					if pt.Kind == TokenIdentifier && pt.Text == "end" {
						tokenizer.Consume()
						break
					}
					if pt.Kind == TokenSymbol && pt.Text == "]" {
						if name == "[" {
							tokenizer.Consume()
						}
						break
					}

					val, err := e.evalExpr(tokenizer, elemType)
					if err != nil {
						return nil, err
					}

					vArg := reflect.ValueOf(val)
					vArg = convertType(vArg, elemType)
					args = append(args, vArg)
				}

			} else {
				val, err := e.evalExpr(tokenizer, argType)
				if err != nil {
					return nil, err
				}
				vArg := reflect.ValueOf(val)
				vArg = convertType(vArg, argType)
				args = append(args, vArg)
			}

		}

		results := method.Call(args)
		if len(results) == 0 {
			return nil, nil
		}

		last := results[len(results)-1]
		if last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !last.IsNil() {
				return nil, last.Interface().(error)
			}
			if len(results) > 1 {
				return results[0].Interface(), nil
			}
			return nil, nil
		}
		return results[0].Interface(), nil
	}

	return nil, fmt.Errorf("unexpected token kind: %v", t.Kind)
}

func findField(v reflect.Value, name string) reflect.Value {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("tai")
		if tag == name {
			return v.Field(i)
		}
		if strings.EqualFold(f.Name, name) {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

func setField(v reflect.Value, val any) error {
	valV := reflect.ValueOf(val)
	valV = convertType(valV, v.Type())
	if !valV.Type().AssignableTo(v.Type()) {
		return fmt.Errorf("cannot assign %s to %s", valV.Type(), v.Type())
	}
	v.Set(valV)
	return nil
}

func convertType(v reflect.Value, t reflect.Type) reflect.Value {
	if v.Type() == t {
		return v
	}
	if v.Kind() == reflect.Int && t.Kind() == reflect.Float64 {
		return reflect.ValueOf(float64(v.Int()))
	}
	if v.Kind() == reflect.Float64 && t.Kind() == reflect.Int {
		return reflect.ValueOf(int(v.Float()))
	}
	if t.Kind() == reflect.Interface {
		return v
	}
	return v
}
