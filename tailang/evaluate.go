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

func (e *Env) evalExpr(tokenizer TokenStream, expectedType reflect.Type) (_ any, err error) {
	lhs, err := e.evalTerm(tokenizer, expectedType, nil, false, false, 0)
	if err != nil {
		return nil, err
	}

	for {
		t, err := tokenizer.Current()
		if err != nil || t.Kind == TokenEOF {
			break
		}
		if t.Kind == TokenSymbol {
			if strings.HasPrefix(t.Text, "|") {
				pipeLast := t.Text == "|>"
				pipeIndex := 0
				if !pipeLast && len(t.Text) > 1 {
					n, err := strconv.Atoi(t.Text[1:])
					if err == nil && n > 0 {
						pipeIndex = n - 1
					}
				}
				tokenizer.Consume()
				lhs, err = e.evalTerm(tokenizer, expectedType, lhs, true, pipeLast, pipeIndex)
				if err != nil {
					return nil, err
				}
			} else if t.Text == ":" {
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

func (e *Env) evalTerm(tokenizer TokenStream, expectedType reflect.Type, pipedVal any, hasPipe bool, pipeLast bool, pipeIndex int) (_ any, err error) {
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
		if hasPipe {
			return nil, fmt.Errorf("cannot pipe into string literal")
		}
		tokenizer.Consume()
		return t.Text, nil
	}

	if t.Kind == TokenNumber {
		if hasPipe {
			return nil, fmt.Errorf("cannot pipe into number literal")
		}
		tokenizer.Consume()
		if strings.ContainsAny(t.Text, ".eE") {
			f, err := strconv.ParseFloat(t.Text, 64)
			if err == nil && !math.IsInf(f, 0) {
				return f, nil
			}
			bf, _, err := big.ParseFloat(t.Text, 10, 128, big.ToNearestEven)
			if err == nil {
				return bf, nil
			}
			return nil, err
		}
		i, err := strconv.ParseInt(t.Text, 10, 0)
		if err == nil {
			return int(i), nil
		}
		bi := new(big.Int)
		if _, ok := bi.SetString(t.Text, 10); ok {
			return bi, nil
		}
		return nil, fmt.Errorf("invalid number: %s", t.Text)
	}

	if t.Kind == TokenSymbol && t.Text == "(" {
		if hasPipe {
			return nil, fmt.Errorf("cannot pipe into parenthesized expression")
		}
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

	if t.Kind == TokenIdentifier || t.Kind == TokenSymbol || t.Kind == TokenUnquotedString {
		return e.evalCall(tokenizer, t, expectedType, pipedVal, hasPipe, pipeLast, pipeIndex)
	}

	return nil, fmt.Errorf("unexpected token kind: %v", t.Kind)
}

func (e *Env) evalCall(tokenizer TokenStream, t *Token, expectedType reflect.Type, pipedVal any, hasPipe bool, pipeLast bool, pipeIndex int) (any, error) {
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
		if t.Kind == TokenUnquotedString {
			if hasPipe {
				return nil, fmt.Errorf("cannot pipe into unquoted string")
			}
			return name, nil
		}
		return nil, fmt.Errorf("undefined identifier: %s", name)
	}

	if expectedType != nil && expectedType.Kind() == reflect.String {
		if _, ok := val.(string); !ok {
			if hasPipe {
				return nil, fmt.Errorf("cannot pipe into string expected identifier")
			}
			return name, nil
		}
	}

	if isRef {
		if hasPipe {
			return nil, fmt.Errorf("cannot pipe into reference")
		}
		return val, nil
	}

	if val == nil {
		// Check for named params trying to attach to nil
		next, err := tokenizer.Current()
		if err == nil && next.Kind == TokenNamedParam {
			return nil, fmt.Errorf("cannot use named parameter .%s on nil value", strings.TrimPrefix(next.Text, "."))
		}
		if hasPipe {
			return nil, fmt.Errorf("cannot pipe into nil")
		}
		return nil, nil
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
	seenParams := make(map[string]bool)
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

		if seenParams[paramName] {
			return nil, fmt.Errorf("duplicate named parameter: .%s", paramName)
		}
		seenParams[paramName] = true

		if !isWrapped {
			return nil, fmt.Errorf("cannot use named parameter .%s on non-struct type %v", paramName, typ)
		}

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

	if fn, ok := callVal.Interface().(Function); ok {
		stream := tokenizer
		if hasPipe {
			stream = &PipedStream{
				TokenStream: tokenizer,
				Value:       pipedVal,
				HasValue:    true,
				PipeLast:    pipeLast,
				PipeIndex:   pipeIndex,
			}
		}
		return fn.Call(e, stream, expectedType)
	}

	method := callVal.MethodByName("Call")
	if !method.IsValid() {
		if isWrapped {
			if hasPipe {
				return nil, fmt.Errorf("cannot pipe into non-callable struct")
			}
			return callVal.Elem().Interface(), nil
		}
		if hasPipe {
			return nil, fmt.Errorf("cannot pipe into non-callable value")
		}
		return callVal.Interface(), nil
	}

	stream := tokenizer
	if hasPipe {
		stream = &PipedStream{
			TokenStream: tokenizer,
			Value:       pipedVal,
			HasValue:    true,
			PipeLast:    pipeLast,
			PipeIndex:   pipeIndex,
		}
	}

	return e.callFunc(stream, method, name, expectedType)
}

func (e *Env) callFunc(tokenizer TokenStream, fn reflect.Value, name string, expectedType reflect.Type) (any, error) {
	if fn.Kind() != reflect.Func {
		return nil, fmt.Errorf("cannot call non-function type %v", fn.Type())
	}

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

	usePipe := false
	var pipedVal any
	var pipeLast bool
	var pipeIndex int
	if ps, ok := tokenizer.(*PipedStream); ok && ps.HasValue {
		usePipe = true
		pipedVal = ps.Value
		pipeLast = ps.PipeLast
		pipeIndex = ps.PipeIndex
	}

	logicalIdx := 0

	for i := argOffset; i < numIn; i++ {
		argType := methodType.In(i)

		if isVariadic && i == numIn-1 {
			elemType := argType.Elem()

			for {
				if usePipe && !pipeLast && logicalIdx == pipeIndex {
					vArg, err := prepareAssign(pipedVal, elemType)
					if err != nil {
						return nil, err
					}
					args = append(args, vArg)
					usePipe = false
					logicalIdx++
					continue
				}

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
				logicalIdx++
			}

			if usePipe && pipeLast {
				vArg, err := prepareAssign(pipedVal, elemType)
				if err != nil {
					return nil, err
				}
				args = append(args, vArg)
				usePipe = false
				logicalIdx++
			}

		} else {
			var val any
			var err error

			if usePipe && !pipeLast && logicalIdx == pipeIndex {
				val = pipedVal
				usePipe = false
			} else if usePipe && pipeLast && i == numIn-1 {
				val = pipedVal
				usePipe = false
			} else {
				val, err = e.evalExpr(tokenizer, argType)
				if err != nil {
					return nil, err
				}
			}

			vArg, err := prepareAssign(val, argType)
			if err != nil {
				return nil, err
			}
			args = append(args, vArg)
			logicalIdx++
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
			convElem, err := prepareAssign(elemVal, elemType)
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
