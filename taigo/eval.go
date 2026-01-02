package taigo

import (
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"reflect"

	"github.com/reusee/tai/taivm"
)

func Eval[T any](env *taivm.Env, src any) (ret T, err error) {
	val, err := evalSrc(env, src)
	if err != nil {
		return ret, err
	}
	if val == nil {
		return ret, nil
	}

	if v, ok := val.(T); ok {
		return v, nil
	}
	targetType := reflect.TypeFor[T]()
	res := convertToReflectValue(env, val, targetType)
	if v, ok := res.Interface().(T); ok {
		return v, nil
	}
	return ret, fmt.Errorf("cannot convert %T to %v", val, targetType)
}

func evalSrc(env *taivm.Env, src any) (any, error) {
	var srcStr string
	switch s := src.(type) {
	case string:
		srcStr = s
	case []byte:
		srcStr = string(s)
	case io.Reader:
		b, err := io.ReadAll(s)
		if err != nil {
			return nil, err
		}
		srcStr = string(b)
	default:
		srcStr = fmt.Sprint(s)
	}

	// Extract external types and value types from the runtime environment
	externalTypes := make(map[string]*taivm.Type)
	externalValueTypes := make(map[string]*taivm.Type)
	if env != nil {
		for e := env; e != nil; e = e.Parent {
			for _, v := range e.Vars {
				if _, ok := externalTypes[v.Name]; ok {
					continue
				}
				if _, ok := externalValueTypes[v.Name]; ok {
					continue
				}
				if t, ok := v.Val.(*taivm.Type); ok {
					externalTypes[v.Name] = t
				} else if rt, ok := v.Val.(reflect.Type); ok {
					externalTypes[v.Name] = taivm.FromReflectType(rt)
				} else if v.Type != nil {
					externalValueTypes[v.Name] = v.Type
				} else if v.Val != nil {
					externalValueTypes[v.Name] = taivm.FromReflectType(reflect.TypeOf(v.Val))
				}
			}
		}
	}

	fset := token.NewFileSet()
	expr, err := parser.ParseExpr(srcStr)
	if err == nil {
		fn, err := compileExpr(expr, externalTypes, externalValueTypes)
		if err != nil {
			return nil, err
		}
		return eval(env, fn)
	}

	file, err := parser.ParseFile(fset, "eval", srcStr, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	pkg, err := compile(externalTypes, externalValueTypes, file)
	if err != nil {
		return nil, err
	}
	return eval(env, pkg.Init)
}

func TypedEval(env *taivm.Env, src any, typ reflect.Type) (any, error) {
	val, err := evalSrc(env, src)
	if err != nil {
		return nil, err
	}
	if val == nil {
		return reflect.Zero(typ).Interface(), nil
	}
	res := convertToReflectValue(env, val, typ)
	if res.Type().AssignableTo(typ) {
		return res.Interface(), nil
	}
	return nil, fmt.Errorf("cannot convert %T to %v", val, typ)
}

func Get[T any](env *taivm.Env, name string) (T, error) {
	val, ok := env.Get(name)
	if !ok {
		var zero T
		return zero, fmt.Errorf("variable not found: %s", name)
	}
	if v, ok := val.(T); ok {
		return v, nil
	}
	targetType := reflect.TypeFor[T]()
	res := convertToReflectValue(env, val, targetType)
	if v, ok := res.Interface().(T); ok {
		return v, nil
	}
	var zero T
	return zero, fmt.Errorf("cannot convert %T to %v", val, targetType)
}

func eval(env *taivm.Env, fn *taivm.Function) (any, error) {
	newVM := taivm.NewVM(fn)
	newVM.Scope = env
	for _, err := range newVM.Run {
		if err != nil {
			return nil, err
		}
	}
	if newVM.SP > 0 {
		return newVM.OperandStack[newVM.SP-1], nil
	}
	return nil, nil
}

func convertToReflectValue(env *taivm.Env, val any, target reflect.Type) reflect.Value {
	if val == nil {
		return reflect.Zero(target)
	}
	v := reflect.ValueOf(val)
	if v.Type().AssignableTo(target) {
		return v
	}

	if s, ok := val.(*taivm.Struct); ok {
		t := target
		isPtr := false
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
			isPtr = true
		}
		if t.Kind() == reflect.Struct {
			res := reflect.New(t).Elem()
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if f.PkgPath != "" { // Skip unexported fields
					continue
				}
				if fValue, ok := s.Fields[f.Name]; ok {
					fv := convertToReflectValue(env, fValue, f.Type)
					res.Field(i).Set(fv)
				}
			}
			if isPtr {
				return res.Addr()
			}
			return res
		}
	}

	if target.Kind() == reflect.Func {
		fn := reflect.MakeFunc(target, func(args []reflect.Value) []reflect.Value {
			callArgs := make([]any, len(args))
			for i, arg := range args {
				callArgs[i] = arg.Interface()
			}
			callFn := &taivm.Function{
				Code: []taivm.OpCode{
					taivm.OpLoadConst.With(0),
				},
				Constants: []any{val},
			}
			for i := range len(args) {
				callFn.Code = append(callFn.Code, taivm.OpLoadConst.With(i+1))
				callFn.Constants = append(callFn.Constants, callArgs[i])
			}
			callFn.Code = append(callFn.Code, taivm.OpCall.With(len(args)), taivm.OpReturn)
			newVM := taivm.NewVM(callFn)
			newVM.Scope = env
			for _, err := range newVM.Run {
				if err != nil {
					panic(err)
				}
			}
			res := newVM.OperandStack[newVM.SP-1]
			numOut := target.NumOut()
			results := make([]reflect.Value, numOut)
			if numOut == 0 {
				return results
			}
			if numOut == 1 {
				results[0] = convertToReflectValue(env, res, target.Out(0))
				return results
			}
			list := res.(*taivm.List)
			for i := range numOut {
				results[i] = convertToReflectValue(env, list.Elements[i], target.Out(i))
			}
			return results
		})
		return fn
	}

	if list, ok := val.(*taivm.List); ok && target.Kind() == reflect.Slice {
		slice := reflect.MakeSlice(target, len(list.Elements), len(list.Elements))
		for i, e := range list.Elements {
			slice.Index(i).Set(convertToReflectValue(env, e, target.Elem()))
		}
		return slice
	}

	if v.Type().ConvertibleTo(target) {
		return v.Convert(target)
	}
	return v
}
