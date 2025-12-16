package tailang

import "fmt"

func (v *VM) Run(
	yield func(*Interrupt, error) bool,
) {

	for {
		if v.State.IP >= len(v.State.CurrentFun.Code) {
			return
		}

		op := v.State.CurrentFun.Code[v.State.IP]
		v.State.IP++

		switch op {

		case OpLoadConst:
			idx := v.readUint16()
			v.push(v.State.CurrentFun.Constants[idx])

		case OpLoadVar:
			idx := v.readUint16()
			name := v.State.CurrentFun.Constants[idx].(string)
			val, ok := v.State.Scope.Get(name)
			if !ok {
				if !yield(nil, fmt.Errorf("undefined variable: %s", name)) {
					return
				}
				// If yield continues, we assume the error is handled or we stop.
				// For undefined var, we probably push nil to avoid panic if continuation is forced
				v.push(nil)
				continue
			}
			v.push(val)

		case OpDefVar:
			idx := v.readUint16()
			name := v.State.CurrentFun.Constants[idx].(string)
			val := v.pop()
			v.State.Scope.Def(name, val)

		case OpSetVar:
			idx := v.readUint16()
			name := v.State.CurrentFun.Constants[idx].(string)
			val := v.pop()
			if !v.State.Scope.Set(name, val) {
				if !yield(nil, fmt.Errorf("variable not found: %s", name)) {
					return
				}
			}

		case OpPop:
			v.pop()

		case OpJump:
			offset := int16(v.readUint16())
			v.State.IP += int(offset)

		case OpJumpFalse:
			offset := int16(v.readUint16())
			cond := v.pop()
			if cond == nil || cond == false || cond == 0 || cond == "" {
				v.State.IP += int(offset)
			}

		case OpMakeClosure:
			idx := v.readUint16()
			funVal := v.State.CurrentFun.Constants[idx]
			fun, ok := funVal.(*Function)
			if !ok {
				if !yield(nil, fmt.Errorf("OpMakeClosure: constant at %d is not a Function", idx)) {
					return
				}
				return
			}
			closure := &Closure{
				Fun: fun,
				Env: v.State.Scope,
			}
			v.push(closure)

		case OpCall:
			argc := int(v.readUint16())
			callee := v.pop()

			switch fn := callee.(type) {
			case *Closure:
				if argc != fn.Fun.NumParams {
					// Handle mismatch or varargs if supported later
					// For now, simple strict check could be good, or just use what is passed.
					// Implementation matches provided args to params.
				}

				args := v.popN(argc)

				// Save current context
				v.State.CallStack = append(v.State.CallStack, &Frame{
					ReturnIP: v.State.IP,
					Env:      v.State.Scope,
					Fun:      v.State.CurrentFun,
				})

				// Switch context
				v.State.CurrentFun = fn.Fun
				v.State.IP = 0
				v.State.Scope = fn.Env.NewChild()

				// Bind arguments
				for i := 0; i < argc && i < len(fn.Fun.ParamNames); i++ {
					v.State.Scope.Def(fn.Fun.ParamNames[i], args[i])
				}

			case NativeFunc:
				args := v.popN(argc)
				res, err := fn(v, args)
				if err != nil {
					if !yield(nil, err) {
						return
					}
				}
				v.push(res)

			default:
				if !yield(nil, fmt.Errorf("not callable: %T", callee)) {
					return
				}
			}

		case OpReturn:
			val := v.pop()
			n := len(v.State.CallStack)
			if n == 0 {
				// End of main function or empty stack
				return
			}

			frame := v.State.CallStack[n-1]
			v.State.CallStack = v.State.CallStack[:n-1]

			// Restore context
			v.State.IP = frame.ReturnIP
			v.State.Scope = frame.Env
			v.State.CurrentFun = frame.Fun

			v.push(val)

		case OpSuspend:
			if !yield(InterruptSuspend, nil) {
				return
			}

		case OpEnterScope:
			v.State.Scope = v.State.Scope.NewChild()

		case OpLeaveScope:
			if v.State.Scope.Parent != nil {
				v.State.Scope = v.State.Scope.Parent
			}

		}
	}
}
