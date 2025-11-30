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

type pipeInfo struct {
	val    any
	active bool
	last   bool
	index  int
}

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
	lhs, err := e.evalTerm(tokenizer, expectedType, pipeInfo{})
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
				info := pipeInfo{
					val:    lhs,
					active: true,
					last:   t.Text == "|>",
				}
				if !info.last && len(t.Text) > 1 {
					n, err := strconv.Atoi(t.Text[1:])
					if err == nil && n > 0 {
						info.index = n - 1
					}
				}
				tokenizer.Consume()
				lhs, err = e.evalTerm(tokenizer, expectedType, info)
				if err != nil {
					return nil, err
				}
			} else if t.Text == ":" || t.Text == "::" {
				op := t.Text
				isRef := op == "::"
				tokenizer.Consume()
				// expect identifier
				t, err := tokenizer.Current()
				if err != nil {
					return nil, err
				}
				if t.Kind != TokenIdentifier {
					return nil, fmt.Errorf("expected identifier after %s, got %v", op, t.Kind)
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

				if isRef {
					lhs = method.Interface()
				} else {
					res, err := e.callFunc(tokenizer, method, methodName, nil)
					if err != nil {
						return nil, err
					}
					lhs = res
				}
			} else {
				break
			}
		} else {
			break
		}
	}
	return lhs, nil
}

func (e *Env) evalTerm(tokenizer TokenStream, expectedType reflect.Type, pipe pipeInfo) (any, error) {
	t, err := tokenizer.Current()
	if err != nil {
		return nil, err
	}
	startPos := t.Pos

	checkPipe := func(msg string) error {
		if pipe.active {
			return WithPos(fmt.Errorf("%s", msg), startPos)
		}
		return nil
	}

	switch t.Kind {
	case TokenString:
		if err := checkPipe("cannot pipe into string literal"); err != nil {
			return nil, err
		}
		tokenizer.Consume()
		return t.Text, nil

	case TokenNumber:
		if err := checkPipe("cannot pipe into number literal"); err != nil {
			return nil, err
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
			return nil, WithPos(err, startPos)
		}
		i, err := strconv.ParseInt(t.Text, 10, 0)
		if err == nil {
			return int(i), nil
		}
		bi := new(big.Int)
		if _, ok := bi.SetString(t.Text, 10); ok {
			return bi, nil
		}
		return nil, WithPos(fmt.Errorf("invalid number: %s", t.Text), startPos)

	case TokenSymbol:
		if t.Text == "(" {
			if err := checkPipe("cannot pipe into parenthesized expression"); err != nil {
				return nil, err
			}
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
		res, err := e.evalCall(tokenizer, t, expectedType, pipe)
		if err != nil {
			return nil, WithPos(err, startPos)
		}
		return res, nil
	}

	return nil, WithPos(fmt.Errorf("unexpected token kind: %v", t.Kind), startPos)
}

func (e *Env) evalCall(tokenizer TokenStream, t *Token, expectedType reflect.Type, pipe pipeInfo) (any, error) {
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
			if pipe.active {
				return nil, fmt.Errorf("cannot pipe into unquoted string")
			}
			return name, nil
		}
		return nil, fmt.Errorf("undefined identifier: %s", name)
	}

	if expectedType != nil && expectedType.Kind() == reflect.String {
		if _, ok := val.(string); !ok {
			if pipe.active {
				return nil, fmt.Errorf("cannot pipe into string expected identifier")
			}
			return name, nil
		}
	}

	if isRef {
		if pipe.active {
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
		if pipe.active {
			return nil, fmt.Errorf("cannot pipe into nil")
		}
		return nil, nil
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

		if !isWrapped {
			return nil, fmt.Errorf("cannot use named parameter .%s on non-struct type %v", paramName, v.Type())
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
		stream := tokenizer
		if pipe.active {
			stream = &PipedStream{
				TokenStream: tokenizer,
				Value:       pipe.val,
				HasValue:    true,
				PipeLast:    pipe.last,
				PipeIndex:   pipe.index,
			}
		}
		return fn.Call(e, stream, expectedType)
	}

	method := v.MethodByName("Call")
	if !method.IsValid() {
		if isWrapped {
			if pipe.active {
				return nil, fmt.Errorf("cannot pipe into non-callable struct")
			}
			return v.Elem().Interface(), nil
		}
		if pipe.active {
			return nil, fmt.Errorf("cannot pipe into non-callable value")
		}
		return v.Interface(), nil
	}

	stream := tokenizer
	if pipe.active {
		stream = &PipedStream{
			TokenStream: tokenizer,
			Value:       pipe.val,
			HasValue:    true,
			PipeLast:    pipe.last,
			PipeIndex:   pipe.index,
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

	var pipedVal any
	var pipeLast bool
	var pipeIndex int
	var hasPipe bool
	if ps, ok := tokenizer.(*PipedStream); ok && ps.HasValue {
		hasPipe = true
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
				if hasPipe && !pipeLast && logicalIdx == pipeIndex {
					vArg, err := PrepareAssign(pipedVal, elemType)
					if err != nil {
						return nil, err
					}
					args = append(args, vArg)
					hasPipe = false
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

				vArg, err := PrepareAssign(val, elemType)
				if err != nil {
					return nil, err
				}
				args = append(args, vArg)
				logicalIdx++
			}

			if hasPipe && pipeLast {
				vArg, err := PrepareAssign(pipedVal, elemType)
				if err != nil {
					return nil, err
				}
				args = append(args, vArg)
				hasPipe = false
				logicalIdx++
			}

		} else {
			var val any
			var err error

			if hasPipe && !pipeLast && logicalIdx == pipeIndex {
				val = pipedVal
				hasPipe = false
			} else if hasPipe && pipeLast && i == numIn-1 {
				val = pipedVal
				hasPipe = false
			} else {
				val, err = e.evalExpr(tokenizer, argType)
				if err != nil {
					return nil, err
				}
			}

			vArg, err := PrepareAssign(val, argType)
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
