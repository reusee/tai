package taigo

import (
	"reflect"

	"github.com/reusee/tai/taivm"
)

func Eval[T any](env *taivm.Env, expr string) (ret T, err error) {
	val, err := Exec(env, expr)
	if err != nil {
		return ret, err
	}
	if v, ok := val.(T); ok {
		return v, nil
	}
	targetType := reflect.TypeFor[T]()
	res := convertToReflectValue(env, val, targetType)
	return res.Interface().(T), nil
}

func convertToReflectValue(env *taivm.Env, val any, target reflect.Type) reflect.Value {
	if val == nil {
		return reflect.Zero(target)
	}
	v := reflect.ValueOf(val)
	if v.Type().AssignableTo(target) {
		return v
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

	return v.Convert(target)
}
