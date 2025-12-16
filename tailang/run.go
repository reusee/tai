package tailang

import "fmt"

func (v *VM) Run(yield func(*Interrupt, error) bool) {
	for {
		if v.State.IP < 0 || v.State.IP >= len(v.State.CurrentFun.Code) {
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
			c := v.State.CurrentFun.Constants[idx]
			var sym Symbol
			if s, ok := c.(Symbol); ok {
				sym = s
			} else {
				sym = Intern(c.(string))
				v.State.CurrentFun.Constants[idx] = sym
			}
			val, ok := v.State.Scope.GetSym(sym)
			if !ok {
				//TODO symbol to name
				if !yield(nil, fmt.Errorf("undefined variable")) {
					return
				}
				v.push(nil)
				continue
			}
			v.push(val)

		case OpDefVar:
			idx := v.readUint16()
			c := v.State.CurrentFun.Constants[idx]
			var sym Symbol
			if s, ok := c.(Symbol); ok {
				sym = s
			} else {
				sym = Intern(c.(string))
				v.State.CurrentFun.Constants[idx] = sym
			}
			v.State.Scope.DefSym(sym, v.pop())

		case OpSetVar:
			idx := v.readUint16()
			c := v.State.CurrentFun.Constants[idx]
			var sym Symbol
			if s, ok := c.(Symbol); ok {
				sym = s
			} else {
				sym = Intern(c.(string))
				v.State.CurrentFun.Constants[idx] = sym
			}
			val := v.pop()
			if !v.State.Scope.SetSym(sym, val) {
				if !yield(nil, fmt.Errorf("variable not found")) {
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
			val := v.pop()
			if val == nil || val == false || val == 0 || val == "" {
				v.State.IP += int(offset)
			}

		case OpMakeClosure:
			idx := v.readUint16()
			fun := v.State.CurrentFun.Constants[idx].(*Function)
			v.push(&Closure{Fun: fun, Env: v.State.Scope})

		case OpCall:
			argc := int(v.readUint16())
			if v.State.SP < argc+1 {
				if !yield(nil, fmt.Errorf("stack underflow during call")) {
					return
				}
				continue
			}

			// Callee is below args on the stack
			calleeIdx := v.State.SP - argc - 1
			callee := v.State.OperandStack[calleeIdx]

			switch fn := callee.(type) {
			case *Closure:
				if argc != fn.Fun.NumParams {
					if !yield(nil, fmt.Errorf("arity mismatch: want %d, got %d", fn.Fun.NumParams, argc)) {
						return
					}
					return
				}

				newEnv := fn.Env.NewChild()

				fn.Fun.EnsureParamSymbols()

				// Pre-allocate environment storage to avoid repeated resizing
				var maxSym int = -1
				for _, sym := range fn.Fun.ParamSymbols {
					s := int(sym)
					if s > maxSym {
						maxSym = s
					}
				}
				if maxSym >= 0 {
					newEnv.Grow(maxSym)
				}

				// Bind arguments from stack directly to new environment
				for i := range argc {
					newEnv.DefSym(fn.Fun.ParamSymbols[i], v.State.OperandStack[calleeIdx+1+i])
				}

				// Tail Call Optimization
				if v.State.IP < len(v.State.CurrentFun.Code) && v.State.CurrentFun.Code[v.State.IP] == OpReturn {
					var baseSP int
					if n := len(v.State.CallStack); n > 0 {
						baseSP = v.State.CallStack[n-1].BaseSP
					}
					v.drop(v.State.SP - baseSP)
				} else {
					v.drop(argc + 1)
					v.State.CallStack = append(v.State.CallStack, Frame{
						Fun:      v.State.CurrentFun,
						ReturnIP: v.State.IP,
						Env:      v.State.Scope,
						BaseSP:   v.State.SP,
					})
				}

				v.State.CurrentFun = fn.Fun
				v.State.IP = 0
				v.State.Scope = newEnv

			case NativeFunc:
				// Zero-allocation slice view of arguments
				// Note: Slice is valid only until next Stack modification, which is fine for sync calls
				args := v.State.OperandStack[calleeIdx+1 : v.State.SP]
				res, err := fn(v, args)

				// Cleanup stack after call
				v.drop(argc + 1)

				if err != nil {
					if !yield(nil, err) {
						return
					}
					v.push(nil) // Push nil if error handled
				} else {
					v.push(res)
				}

			default:
				if !yield(nil, fmt.Errorf("calling non-function: %T", callee)) {
					return
				}
				v.drop(argc + 1)
				v.push(nil)
			}

		case OpReturn:
			retVal := v.pop()
			n := len(v.State.CallStack)
			if n == 0 {
				return
			}
			frame := v.State.CallStack[n-1]
			v.State.CallStack = v.State.CallStack[:n-1]

			// Restore Call Frame
			v.State.CurrentFun = frame.Fun
			v.State.IP = frame.ReturnIP
			v.State.Scope = frame.Env
			// Ensure we discard any garbage left on stack by the called function
			v.drop(v.State.SP - frame.BaseSP)

			v.push(retVal)

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

		case OpMakeList:
			n := int(v.readUint16())
			if v.State.SP < n {
				if !yield(nil, fmt.Errorf("stack underflow during list creation")) {
					return
				}
				continue
			}
			slice := make([]any, n)
			start := v.State.SP - n
			copy(slice, v.State.OperandStack[start:v.State.SP])
			v.drop(n)
			v.push(slice)

		case OpMakeMap:
			n := int(v.readUint16())
			if v.State.SP < n*2 {
				if !yield(nil, fmt.Errorf("stack underflow during map creation")) {
					return
				}
				continue
			}
			m := make(map[any]any, n)
			start := v.State.SP - n*2
			for i := 0; i < n; i++ {
				k := v.State.OperandStack[start+i*2]
				val := v.State.OperandStack[start+i*2+1]
				m[k] = val
			}
			v.drop(n * 2)
			v.push(m)

		case OpGetIndex:
			key := v.pop()
			target := v.pop()
			if target == nil {
				if !yield(nil, fmt.Errorf("indexing nil")) {
					return
				}
				v.push(nil)
				continue
			}

			var val any
			switch t := target.(type) {
			case []any:
				var idx int
				var ok bool
				switch i := key.(type) {
				case int:
					idx = i
					ok = true
				case int64:
					idx = int(i)
					ok = true
				}

				if !ok {
					if !yield(nil, fmt.Errorf("slice index must be int, got %T", key)) {
						return
					}
					val = nil
				} else if idx < 0 || idx >= len(t) {
					if !yield(nil, fmt.Errorf("index out of bounds: %d", idx)) {
						return
					}
					val = nil
				} else {
					val = t[idx]
				}

			case map[any]any:
				val = t[key]

			case map[string]any:
				if k, ok := key.(string); ok {
					val = t[k]
				}

			default:
				if !yield(nil, fmt.Errorf("type %T is not indexable", target)) {
					return
				}
			}
			v.push(val)

		case OpSetIndex:
			val := v.pop()
			key := v.pop()
			target := v.pop()

			if target == nil {
				if !yield(nil, fmt.Errorf("assignment to nil")) {
					return
				}
				continue
			}

			switch t := target.(type) {
			case []any:
				var idx int
				var ok bool
				switch i := key.(type) {
				case int:
					idx = i
					ok = true
				case int64:
					idx = int(i)
					ok = true
				}
				if !ok {
					if !yield(nil, fmt.Errorf("slice index must be int, got %T", key)) {
						return
					}
					continue
				}
				if idx < 0 || idx >= len(t) {
					if !yield(nil, fmt.Errorf("index out of bounds: %d", idx)) {
						return
					}
					continue
				}
				t[idx] = val

			case map[any]any:
				t[key] = val

			case map[string]any:
				if k, ok := key.(string); ok {
					t[k] = val
				} else {
					if !yield(nil, fmt.Errorf("map key must be string, got %T", key)) {
						return
					}
				}

			default:
				if !yield(nil, fmt.Errorf("type %T does not support assignment", target)) {
					return
				}
			}

		case OpSwap:
			if v.State.SP < 2 {
				if !yield(nil, fmt.Errorf("stack underflow during swap")) {
					return
				}
				continue
			}
			top := v.State.SP - 1
			under := v.State.SP - 2
			v.State.OperandStack[top], v.State.OperandStack[under] = v.State.OperandStack[under], v.State.OperandStack[top]
		}
	}
}
