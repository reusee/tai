package taipy

import (
	"fmt"

	"github.com/reusee/tai/taivm"
)

var ApplyKw = taivm.NativeFunc{
	Name: "__apply_kw",
	Func: func(vm *taivm.VM, args []any) (any, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("__apply_kw expects 3 arguments")
		}
		fnObj := args[0]
		posArgs, ok := args[1].([]any)
		if !ok {
			return nil, fmt.Errorf("pos_args must be list")
		}
		kwArgs, ok := args[2].(map[any]any)
		if !ok {
			return nil, fmt.Errorf("kw_args must be map")
		}

		switch fn := fnObj.(type) {
		case *taivm.Closure:
			numParams := fn.Fun.NumParams
			paramNames := fn.Fun.ParamNames
			isVariadic := fn.Fun.Variadic

			newEnv := fn.Env.NewChild()
			paramSyms := fn.ParamSyms
			maxSym := fn.MaxParamSym
			if len(paramSyms) == 0 && len(paramNames) > 0 {
				paramSyms = make([]taivm.Symbol, len(paramNames))
				for i, name := range paramNames {
					sym := vm.Intern(name)
					paramSyms[i] = sym
					if int(sym) > maxSym {
						maxSym = int(sym)
					}
				}
				fn.ParamSyms = paramSyms
				fn.MaxParamSym = maxSym
			}
			if len(paramSyms) > 0 {
				newEnv.Grow(maxSym)
			}

			if len(posArgs) > numParams && !isVariadic {
				return nil, fmt.Errorf("too many arguments: want %d, got %d", numParams, len(posArgs))
			}

			isSet := make([]bool, numParams)
			nPos := len(posArgs)
			if isVariadic && nPos > numParams-1 {
				nPos = numParams - 1
			}

			for i := 0; i < nPos; i++ {
				newEnv.DefSym(paramSyms[i], posArgs[i])
				isSet[i] = true
			}

			for k, v := range kwArgs {
				name, ok := k.(string)
				if !ok {
					return nil, fmt.Errorf("keyword must be string")
				}
				found := false
				for i, pname := range paramNames {
					if pname == name {
						if isVariadic && i == numParams-1 {
							continue
						}
						if isSet[i] {
							return nil, fmt.Errorf("multiple values for argument '%s'", name)
						}
						newEnv.DefSym(paramSyms[i], v)
						isSet[i] = true
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("unexpected keyword argument '%s'", name)
				}
			}

			checkLimit := numParams
			if isVariadic {
				checkLimit = numParams - 1
			}
			for i := 0; i < checkLimit; i++ {
				if !isSet[i] {
					return nil, fmt.Errorf("missing argument '%s'", paramNames[i])
				}
			}

			if isVariadic {
				var extra []any
				if len(posArgs) > numParams-1 {
					extra = posArgs[numParams-1:]
				} else {
					extra = []any{}
				}
				newEnv.DefSym(paramSyms[numParams-1], extra)
			}

			calleeIdx := vm.SP - len(args) - 1
			vm.CallStack = append(vm.CallStack, taivm.Frame{
				Fun:      vm.CurrentFun,
				ReturnIP: vm.IP,
				Env:      vm.Scope,
				BaseSP:   calleeIdx,
				BP:       vm.BP,
			})

			vm.CurrentFun = fn.Fun
			vm.IP = 0
			vm.Scope = newEnv
			vm.BP = calleeIdx + 1

			return nil, nil

		case taivm.NativeFunc:
			if len(kwArgs) > 0 {
				return nil, fmt.Errorf("native functions do not support keyword arguments")
			}
			return fn.Func(vm, posArgs)

		default:
			return nil, fmt.Errorf("not a function: %T", fnObj)
		}
	},
}

var Concat = taivm.NativeFunc{
	Name: "concat",
	Func: func(vm *taivm.VM, args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("concat expects 2 arguments")
		}
		l1, ok1 := args[0].([]any)
		l2, ok2 := args[1].([]any)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("concat operands must be lists")
		}
		res := make([]any, 0, len(l1)+len(l2))
		res = append(res, l1...)
		res = append(res, l2...)
		return res, nil
	},
}
