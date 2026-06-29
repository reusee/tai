package generators

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type Function struct {
	Decl FuncDecl
	Func func(args map[string]any) (map[string]any, error)
}

func MakeFunc(name string, fn any) (*Function, error) {
	fnVal := reflect.ValueOf(fn)
	fnType := fnVal.Type()
	if fnType.Kind() != reflect.Func {
		return nil, fmt.Errorf("MakeFunc: expected a function, got %T", fn)
	}

	var params Vars
	paramNames := make([]string, fnType.NumIn())
	for i := 0; i < fnType.NumIn(); i++ {
		paramType := fnType.In(i)
		paramName := fmt.Sprintf("arg%d", i)
		paramNames[i] = paramName
		var paramVar Var
		if paramType.Kind() == reflect.Pointer {
			elemVar := toVar(paramType.Elem(), paramName)
			elemVar.Optional = true
			paramVar = elemVar
		} else {
			paramVar = toVar(paramType, paramName)
		}
		params = append(params, paramVar)
	}

	numOut := fnType.NumOut()
	hasErrorReturn := false
	if numOut > 0 {
		lastType := fnType.Out(numOut - 1)
		if lastType == reflect.TypeFor[error]() {
			hasErrorReturn = true
		}
	}
	var returns Vars
	resultIdx := 0
	for i := range numOut {
		outType := fnType.Out(i)
		if hasErrorReturn && i == numOut-1 {
			continue
		}
		retName := fmt.Sprintf("result%d", resultIdx)
		resultIdx++
		retVar := toVar(outType, retName)
		returns = append(returns, retVar)
	}

	callFunc := func(args map[string]any) (map[string]any, error) {
		inVals := make([]reflect.Value, len(paramNames))
		for i, pName := range paramNames {
			pType := fnType.In(i)
			val, ok := args[pName]
			if !ok {
				return nil, fmt.Errorf("missing argument: %s", pName)
			}
			converted, err := convertMapValue(val, pType)
			if err != nil {
				return nil, fmt.Errorf("converting argument %q: %w", pName, err)
			}
			inVals[i] = converted
		}
		outVals := fnVal.Call(inVals)
		resultMap := make(map[string]any)
		resultIdx := 0
		for i, outVal := range outVals {
			if hasErrorReturn && i == numOut-1 {
				if !outVal.IsNil() {
					return nil, outVal.Interface().(error)
				}
				continue
			}
			key := fmt.Sprintf("result%d", resultIdx)
			resultIdx++
			resultMap[key] = outVal.Interface()
		}
		return resultMap, nil
	}

	return &Function{
		Decl: FuncDecl{
			Name:    name,
			Params:  params,
			Returns: returns,
		},
		Func: callFunc,
	}, nil
}

func toVar(t reflect.Type, name string) Var {
	var v Var
	v.Name = name
	switch t.Kind() {
	case reflect.String:
		v.Type = TypeString
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v.Type = TypeInteger
	case reflect.Float32, reflect.Float64:
		v.Type = TypeNumber
	case reflect.Bool:
		v.Type = TypeBoolean
	case reflect.Slice, reflect.Array:
		v.Type = TypeArray
		elemVar := toVar(t.Elem(), "element")
		v.ItemType = &elemVar
	case reflect.Map:
		v.Type = TypeObject
	case reflect.Struct:
		v.Type = TypeObject
		var props Vars
		for field := range t.Fields() {
			if !field.IsExported() {
				continue
			}
			fieldVar := toVar(field.Type, field.Name)
			props = append(props, fieldVar)
		}
		v.Properties = props
	case reflect.Pointer:
		elemVar := toVar(t.Elem(), name)
		elemVar.Optional = true
		return elemVar
	case reflect.Interface:
		v.Type = TypeString
	default:
		v.Type = TypeString
	}
	return v
}

func convertMapValue(val any, targetType reflect.Type) (reflect.Value, error) {
	data, err := json.Marshal(val)
	if err != nil {
		return reflect.Value{}, err
	}
	var targetPtr reflect.Value
	if targetType.Kind() == reflect.Pointer {
		targetPtr = reflect.New(targetType.Elem())
	} else {
		targetPtr = reflect.New(targetType)
	}
	if err := json.Unmarshal(data, targetPtr.Interface()); err != nil {
		return reflect.Value{}, err
	}
	if targetType.Kind() == reflect.Pointer {
		return targetPtr, nil
	}
	return targetPtr.Elem(), nil
}
