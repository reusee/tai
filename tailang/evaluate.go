package tailang

import (
	"fmt"
	"io"
	"math"
	"math/big"
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
	lhs, err := e.evalTerm(tokenizer, expectedType)
	if err != nil {
		return nil, err
	}

	for {
		t, err := tokenizer.Current()
		if err != nil || t.Kind == TokenEOF {
			break
		}

		if t.Kind == TokenSymbol {
			if t.Text == ":" {
				tokenizer.Consume()
				// expect identifier
				t, err := tokenizer.Current()
				if err != nil {
					return nil, err
				}
				if t.Kind != TokenIdentifier {
					return nil, fmt.Errorf("expected identifier after :, got %v", t.Kind)
				}
				methodName := t.Text
				tokenizer.Consume()

				if lhs == nil {
					return nil, fmt.Errorf("cannot call method %s on nil", methodName)
				}
				v := reflect.ValueOf(lhs)
				method := v.MethodByName(methodName)
				if !method.IsValid() {
					return nil, fmt.Errorf("method %s not found on %T", methodName, lhs)
				}

				res, err := e.callFunc(tokenizer, method, methodName, nil)
				if err != nil {
					return nil, err
				}
				lhs = res
			} else {
				break
			}
		} else {
			break
		}
	}
	return lhs, nil
}

func (e *Env) evalTerm(tokenizer TokenStream, expectedType reflect.Type) (any, error) {
	t, err := tokenizer.Current()
	if err != nil {
		return nil, err
	}
	startPos := t.Pos

	switch t.Kind {
	case TokenString:
		tokenizer.Consume()
		return t.Text, nil

	case TokenNumber:
		tokenizer.Consume()
		if t.Value != nil {
			return t.Value, nil
		}
		// Fallback for safety or compilation (keeps imports used)
		_ = math.NaN()
		_ = big.NewInt(0)
		_ = strconv.IntSize

		return nil, WithPos(fmt.Errorf("invalid number: %s", t.Text), startPos)

	case TokenSymbol:
		if t.Text == "(" {
			tokenizer.Consume()
			val, err := e.evalExpr(tokenizer, expectedType)
			if err != nil {
				return nil, WithPos(err, startPos)
			}
			t, err = tokenizer.Current()
			if err != nil {
				return nil, WithPos(err, startPos)
			}
			if t.Text != ")" {
				return nil, WithPos(fmt.Errorf("expected )"), startPos)
			}
			tokenizer.Consume()
			return val, nil
		}
	}

	if t.Kind == TokenIdentifier || t.Kind == TokenSymbol || t.Kind == TokenUnquotedString {
		res, err := e.evalCall(tokenizer, t, expectedType)
		if err != nil {
			return nil, WithPos(err, startPos)
		}
		return res, nil
	}

	return nil, WithPos(fmt.Errorf("unexpected token kind: %v", t.Kind), startPos)
}

func (e *Env) evalCall(tokenizer TokenStream, t *Token, expectedType reflect.Type) (any, error) {
	name := t.Text
	if t.Kind == TokenSymbol {
		switch name {
		case ")", "]", "}":
			return nil, fmt.Errorf("unexpected symbol '%s'", name)
		}
	}

	tokenizer.Consume()

	val, ok := e.Lookup(name)
	isRef := false

	if !ok && strings.HasPrefix(name, "&") && len(name) > 1 {
		isRef = true
		name = name[1:]
		val, ok = e.Lookup(name)
	}

	if !ok {
		if t.Kind == TokenUnquotedString {
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

	if val == nil {
		// Check for named params trying to attach to nil
		next, err := tokenizer.Current()
		if err == nil && next.Kind == TokenNamedParam {
			return nil, fmt.Errorf("cannot use named parameter .%s on nil value", strings.TrimPrefix(next.Text, "."))
		}
		return nil, nil
	}

	// Optimization: Fast path for Function implementation without named parameters
	if fn, ok := val.(Function); ok {
		next, err := tokenizer.Current()
		if err != nil && err != io.EOF {
			return nil, err
		}
		if next == nil || next.Kind != TokenNamedParam {
			return fn.Call(e, tokenizer, expectedType)
		}
	}

	v := reflect.ValueOf(val)
	isWrapped := false

	// Struct wrapping logic
	if v.Kind() == reflect.Struct {
		shouldWrap := false
		// Peek for named params or if pointer method needed
		next, err := tokenizer.Current()
		if err == nil && next.Kind == TokenNamedParam {
			shouldWrap = true
		} else {
			// Check if we need to wrap because value doesn't implement Call/Function
			// but pointer does.
			_, isFunc := val.(Function)
			hasCall := v.MethodByName("Call").IsValid()
			if !isFunc && !hasCall {
				if _, ok := reflect.PointerTo(v.Type()).MethodByName("Call"); ok {
					shouldWrap = true
				}
			}
		}

		if shouldWrap {
			ptr := reflect.New(v.Type())
			ptr.Elem().Set(v)
			v = ptr
			isWrapped = true
		}
	}

	// Named parameters
	var seenParams map[string]bool
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

		if seenParams == nil {
			seenParams = make(map[string]bool)
		}

		paramName := strings.TrimPrefix(next.Text, ".")
		tokenizer.Consume()

		if seenParams[paramName] {
			return nil, fmt.Errorf("duplicate named parameter: .%s", paramName)
		}
		seenParams[paramName] = true

		// Ensure v is a pointer to a struct and not nil.
		if v.Kind() != reflect.Ptr {
			return nil, fmt.Errorf("cannot use named parameter .%s on non-pointer type %v", paramName, v.Type())
		}
		if v.IsNil() {
			return nil, fmt.Errorf("cannot use named parameter .%s on nil pointer", paramName)
		}
		if v.Elem().Kind() != reflect.Struct {
			return nil, fmt.Errorf("cannot use named parameter .%s on non-struct pointer type %v", paramName, v.Type())
		}
		field := findField(v.Elem(), paramName)
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

	if fn, ok := v.Interface().(Function); ok {
		return fn.Call(e, tokenizer, expectedType)
	}

	method := v.MethodByName("Call")
	if !method.IsValid() {
		if isWrapped {
			return v.Elem().Interface(), nil
		}
		return v.Interface(), nil
	}

	return e.callFunc(tokenizer, method, name, expectedType)
}

func (e *Env) callFunc(tokenizer TokenStream, fn reflect.Value, name string, expectedType reflect.Type) (any, error) {
	if fn.Kind() != reflect.Func {
		return nil, fmt.Errorf("cannot call non-function type %v", fn.Type())
	}

	methodType := fn.Type()
	numIn := methodType.NumIn()
	isVariadic := methodType.IsVariadic()
	args := make([]reflect.Value, 0, numIn)

	for i := 0; i < numIn; i++ {
		argType := methodType.In(i)

		val, err := e.evalExpr(tokenizer, argType)
		if err != nil {
			return nil, err
		}

		vArg, err := PrepareAssign(val, argType)
		if err != nil {
			return nil, err
		}
		args = append(args, vArg)
	}

	var results []reflect.Value
	if isVariadic {
		results = fn.CallSlice(args)
	} else {
		results = fn.Call(args)
	}

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
	valV, err := PrepareAssign(val, v.Type())
	if err != nil {
		return err
	}
	v.Set(valV)
	return nil
}

func ConvertType(v reflect.Value, t reflect.Type) reflect.Value {
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
								valV = ConvertType(valV, t.Out(0))
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
							valV = ConvertType(valV, t.Out(0))
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

	if IsNumeric(v.Kind()) && IsNumeric(t.Kind()) {
		if v.CanConvert(t) {
			return v.Convert(t)
		}
	}

	if v.Kind() == reflect.String && t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		return reflect.ValueOf([]byte(v.String()))
	}
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 && t.Kind() == reflect.String {
		return reflect.ValueOf(string(v.Bytes()))
	}

	if v.Kind() == reflect.Slice && t.Kind() == reflect.Slice {
		newSlice := reflect.MakeSlice(t, v.Len(), v.Len())
		elemType := t.Elem()
		ok := true
		for i := 0; i < v.Len(); i++ {
			elemVal := v.Index(i).Interface()
			convElem, err := PrepareAssign(elemVal, elemType)
			if err != nil {
				ok = false
				break
			}
			newSlice.Index(i).Set(convElem)
		}
		if ok {
			return newSlice
		}
	}

	if t.Kind() == reflect.Slice && v.Kind() != reflect.Slice {
		elemType := t.Elem()
		valV := ConvertType(v, elemType)
		if valV.Type().AssignableTo(elemType) {
			newSlice := reflect.MakeSlice(t, 1, 1)
			newSlice.Index(0).Set(valV)
			return newSlice
		}
	}

	return v
}

func IsNumeric(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func PrepareAssign(val any, targetType reflect.Type) (reflect.Value, error) {
	if val == nil {
		switch targetType.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			return reflect.Zero(targetType), nil
		default:
			return reflect.Value{}, fmt.Errorf("cannot assign nil to %v", targetType)
		}
	}

	valV := reflect.ValueOf(val)
	valV = ConvertType(valV, targetType)
	if !valV.Type().AssignableTo(targetType) {
		return reflect.Value{}, fmt.Errorf("cannot assign %v (type %v) to %v", val, valV.Type(), targetType)
	}
	return valV, nil
}
