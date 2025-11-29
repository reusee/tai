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

func (e *Env) evalExpr(tokenizer TokenStream, expectedType reflect.Type) (_ any, err error) {
	t, err := tokenizer.Current()
	if err != nil {
		return nil, err
	}
	startPos := t.Pos

	defer func() {
		if err != nil {
			err = WithPos(err, startPos)
		}
	}()

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

	if t.Kind == TokenSymbol && t.Text == "(" {
		tokenizer.Consume()
		val, err := e.evalExpr(tokenizer, expectedType)
		if err != nil {
			return nil, err
		}
		t, err = tokenizer.Current()
		if err != nil {
			return nil, err
		}
		if t.Text != ")" {
			return nil, fmt.Errorf("expected )")
		}
		tokenizer.Consume()
		return val, nil
	}

	if t.Kind == TokenIdentifier || t.Kind == TokenSymbol {
		return e.evalCall(tokenizer, t, expectedType)
	}

	return nil, fmt.Errorf("unexpected token kind: %v", t.Kind)
}

func (e *Env) evalCall(tokenizer TokenStream, t *Token, expectedType reflect.Type) (any, error) {
	name := t.Text
	if name == "end" {
		return nil, fmt.Errorf("unexpected identifier 'end'")
	}
	if t.Kind == TokenSymbol {
		switch name {
		case ")", "]", "}":
			return nil, fmt.Errorf("unexpected symbol '%s'", name)
		}
	}

	isRef := false
	if strings.HasPrefix(name, "&") && len(name) > 1 {
		isRef = true
		name = name[1:]
	}

	tokenizer.Consume()

	val, ok := e.Lookup(name)
	if !ok {
		if expectedType != nil && expectedType.Kind() == reflect.String {
			return name, nil
		}
		return nil, fmt.Errorf("undefined identifier: %s", name)
	}

	if expectedType != nil && expectedType.Kind() == reflect.String {
		if _, ok := val.(string); !ok {
			return name, nil
		}
	}

	if isRef {
		return val, nil
	}

	v := reflect.ValueOf(val)
	typ := v.Type()

	var callVal reflect.Value
	var isWrapped bool
	if typ.Kind() == reflect.Struct {
		ptr := reflect.New(typ)
		ptr.Elem().Set(v)
		callVal = ptr
		isWrapped = true
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
			return nil, fmt.Errorf("unknown named parameter .%s", paramName)
		}

		if field.Kind() == reflect.Bool {
			field.SetBool(true)
		} else {
			arg, err := e.evalExpr(tokenizer, field.Type())
			if err != nil {
				return nil, fmt.Errorf("arg .%s: %w", paramName, err)
			}
			if err := setField(field, arg); err != nil {
				return nil, fmt.Errorf("arg .%s: %w", paramName, err)
			}
		}
	}

	method := callVal.MethodByName("Call")
	if !method.IsValid() {
		if isWrapped {
			return callVal.Elem().Interface(), nil
		}
		return callVal.Interface(), nil
	}

	return e.callFunc(tokenizer, method, name, expectedType)
}

func (e *Env) callFunc(tokenizer TokenStream, fn reflect.Value, name string, expectedType reflect.Type) (any, error) {
	methodType := fn.Type()
	numIn := methodType.NumIn()
	isVariadic := methodType.IsVariadic()
	args := make([]reflect.Value, 0, numIn)

	argOffset := 0
	if numIn > 0 && methodType.In(0) == reflect.TypeOf(e) {
		args = append(args, reflect.ValueOf(e))
		argOffset++
	}

	hasStream := false
	if numIn > argOffset && methodType.In(argOffset) == reflect.TypeOf((*TokenStream)(nil)).Elem() {
		args = append(args, reflect.ValueOf(tokenizer))
		argOffset++
		hasStream = true
	}

	if numIn > argOffset && methodType.In(argOffset) == reflect.TypeOf((*reflect.Type)(nil)).Elem() {
		if expectedType == nil {
			args = append(args, reflect.Zero(methodType.In(argOffset)))
		} else {
			args = append(args, reflect.ValueOf(expectedType))
		}
		argOffset++
	}

	if hasStream {
		if len(args) == numIn {
			results := fn.Call(args)
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
				if pt.Kind == TokenSymbol {
					if pt.Text == "]" {
						if name == "[" {
							tokenizer.Consume()
						}
						break
					}
					if pt.Text == ")" {
						break
					}
				}

				val, err := e.evalExpr(tokenizer, elemType)
				if err != nil {
					return nil, err
				}

				vArg, err := prepareAssign(val, elemType)
				if err != nil {
					return nil, err
				}
				args = append(args, vArg)
			}

		} else {
			val, err := e.evalExpr(tokenizer, argType)
			if err != nil {
				return nil, err
			}

			vArg, err := prepareAssign(val, argType)
			if err != nil {
				return nil, err
			}
			args = append(args, vArg)
		}

	}

	results := fn.Call(args)
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

func findField(v reflect.Value, name string) reflect.Value {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("tai")
		if tag == name {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

func setField(v reflect.Value, val any) error {
	valV, err := prepareAssign(val, v.Type())
	if err != nil {
		return err
	}
	v.Set(valV)
	return nil
}

func convertType(v reflect.Value, t reflect.Type) reflect.Value {
	if v.Type() == t {
		return v
	}
	if t.Kind() == reflect.Interface {
		return v
	}

	if t.Kind() == reflect.Func {
		if uf, ok := v.Interface().(UserFunc); ok {
			return reflect.MakeFunc(t, func(args []reflect.Value) (results []reflect.Value) {
				funcArgs := make([]any, 0, len(args))
				for _, arg := range args {
					funcArgs = append(funcArgs, arg.Interface())
				}

				res, err := uf.CallArgs(funcArgs)

				numOut := t.NumOut()
				results = make([]reflect.Value, numOut)

				// Check if the last return value is an error
				var returnsError bool
				if numOut > 0 {
					lastType := t.Out(numOut - 1)
					if lastType.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
						returnsError = true
					}
				}

				if err != nil {
					if returnsError {
						// Fill zeroes for non-error returns
						for i := 0; i < numOut-1; i++ {
							results[i] = reflect.Zero(t.Out(i))
						}
						results[numOut-1] = reflect.ValueOf(err)
						return
					} else {
						// Go function does not return error, but we have one.
						panic(fmt.Sprintf("call to %s failed: %v", uf.FunctionName(), err))
					}
				}

				// Assign result
				if numOut > 0 {
					if returnsError {
						// Last is error, set it to nil
						results[numOut-1] = reflect.Zero(t.Out(numOut - 1))

						if numOut > 1 {
							// Assign return value
							var valV reflect.Value
							if res == nil {
								valV = reflect.Zero(t.Out(0))
							} else {
								valV = reflect.ValueOf(res)
								valV = convertType(valV, t.Out(0))
							}
							results[0] = valV
							// Fill middles with zero
							for i := 1; i < numOut-1; i++ {
								results[i] = reflect.Zero(t.Out(i))
							}
						}
					} else {
						// Does not return error
						var valV reflect.Value
						if res == nil {
							valV = reflect.Zero(t.Out(0))
						} else {
							valV = reflect.ValueOf(res)
							valV = convertType(valV, t.Out(0))
						}
						results[0] = valV
						// Fill remaining with zero
						for i := 1; i < numOut; i++ {
							results[i] = reflect.Zero(t.Out(i))
						}
					}
				}

				return results
			})
		}
	}

	if isNumeric(v.Kind()) && isNumeric(t.Kind()) {
		if v.CanConvert(t) {
			return v.Convert(t)
		}
	}
	return v
}

func isNumeric(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func prepareAssign(val any, targetType reflect.Type) (reflect.Value, error) {
	if val == nil {
		switch targetType.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			return reflect.Zero(targetType), nil
		default:
			return reflect.Value{}, fmt.Errorf("cannot assign nil to %v", targetType)
		}
	}

	valV := reflect.ValueOf(val)
	valV = convertType(valV, targetType)
	if !valV.Type().AssignableTo(targetType) {
		return reflect.Value{}, fmt.Errorf("cannot assign %v (type %v) to %v", val, valV.Type(), targetType)
	}
	return valV, nil
}
