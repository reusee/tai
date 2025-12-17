package tailang

import "fmt"

func (v *VM) Run(yield func(*Interrupt, error) bool) {
	for {
		if v.IP < 0 || v.IP >= len(v.CurrentFun.Code) {
			return
		}

		op := v.CurrentFun.Code[v.IP]
		v.IP++

		switch op {
		case OpLoadConst:
			idx := v.readUint16()
			v.push(v.CurrentFun.Constants[idx])

		case OpLoadVar:
			idx := v.readUint16()
			c := v.CurrentFun.Constants[idx]
			var sym Symbol
			if s, ok := c.(Symbol); ok {
				sym = s
			} else {
				sym = v.Intern(c.(string))
				v.CurrentFun.Constants[idx] = sym
			}
			val, ok := v.Scope.GetSym(sym)
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
			c := v.CurrentFun.Constants[idx]
			var sym Symbol
			if s, ok := c.(Symbol); ok {
				sym = s
			} else {
				sym = v.Intern(c.(string))
				v.CurrentFun.Constants[idx] = sym
			}
			v.Scope.DefSym(sym, v.pop())

		case OpSetVar:
			idx := v.readUint16()
			c := v.CurrentFun.Constants[idx]
			var sym Symbol
			if s, ok := c.(Symbol); ok {
				sym = s
			} else {
				sym = v.Intern(c.(string))
				v.CurrentFun.Constants[idx] = sym
			}
			val := v.pop()
			if !v.Scope.SetSym(sym, val) {
				if !yield(nil, fmt.Errorf("variable not found")) {
					return
				}
			}

		case OpPop:
			v.pop()

		case OpJump:
			offset := int16(v.readUint16())
			v.IP += int(offset)

		case OpJumpFalse:
			offset := int16(v.readUint16())
			val := v.pop()
			if val == nil || val == false || val == 0 || val == "" {
				v.IP += int(offset)
			}

		case OpMakeClosure:
			idx := v.readUint16()
			fun := v.CurrentFun.Constants[idx].(*Function)
			v.push(&Closure{Fun: fun, Env: v.Scope})

		case OpCall:
			argc := int(v.readUint16())
			if v.SP < argc+1 {
				if !yield(nil, fmt.Errorf("stack underflow during call")) {
					return
				}
				continue
			}

			// Callee is below args on the stack
			calleeIdx := v.SP - argc - 1
			callee := v.OperandStack[calleeIdx]

			switch fn := callee.(type) {
			case *Closure:
				if argc != fn.Fun.NumParams {
					if !yield(nil, fmt.Errorf("arity mismatch: want %d, got %d", fn.Fun.NumParams, argc)) {
						return
					}
					return
				}

				newEnv := fn.Env.NewChild()

				fn.Fun.EnsureParamSymbols(v.Symbols)

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
					newEnv.DefSym(fn.Fun.ParamSymbols[i], v.OperandStack[calleeIdx+1+i])
				}

				// Tail Call Optimization
				if v.IP < len(v.CurrentFun.Code) && v.CurrentFun.Code[v.IP] == OpReturn {
					var baseSP int
					if n := len(v.CallStack); n > 0 {
						baseSP = v.CallStack[n-1].BaseSP
					}
					v.drop(v.SP - baseSP)
				} else {
					v.drop(argc + 1)
					v.CallStack = append(v.CallStack, Frame{
						Fun:      v.CurrentFun,
						ReturnIP: v.IP,
						Env:      v.Scope,
						BaseSP:   v.SP,
					})
				}

				v.CurrentFun = fn.Fun
				v.IP = 0
				v.Scope = newEnv

			case NativeFunc:
				// Zero-allocation slice view of arguments
				// Note: Slice is valid only until next Stack modification, which is fine for sync calls
				args := v.OperandStack[calleeIdx+1 : v.SP]
				res, err := fn.Func(v, args)

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
			n := len(v.CallStack)
			if n == 0 {
				return
			}
			frame := v.CallStack[n-1]
			v.CallStack = v.CallStack[:n-1]

			// Restore Call Frame
			v.CurrentFun = frame.Fun
			v.IP = frame.ReturnIP
			v.Scope = frame.Env
			// Ensure we discard any garbage left on stack by the called function
			v.drop(v.SP - frame.BaseSP)

			v.push(retVal)

		case OpSuspend:
			if !yield(InterruptSuspend, nil) {
				return
			}

		case OpEnterScope:
			v.Scope = v.Scope.NewChild()

		case OpLeaveScope:
			if v.Scope.Parent != nil {
				v.Scope = v.Scope.Parent
			}

		case OpMakeList:
			n := int(v.readUint16())
			if v.SP < n {
				if !yield(nil, fmt.Errorf("stack underflow during list creation")) {
					return
				}
				continue
			}
			slice := make([]any, n)
			start := v.SP - n
			copy(slice, v.OperandStack[start:v.SP])
			v.drop(n)
			v.push(slice)

		case OpMakeMap:
			n := int(v.readUint16())
			if v.SP < n*2 {
				if !yield(nil, fmt.Errorf("stack underflow during map creation")) {
					return
				}
				continue
			}
			m := make(map[any]any, n)
			start := v.SP - n*2
			for i := range n {
				k := v.OperandStack[start+i*2]
				val := v.OperandStack[start+i*2+1]
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
			if v.SP < 2 {
				if !yield(nil, fmt.Errorf("stack underflow during swap")) {
					return
				}
				continue
			}
			top := v.SP - 1
			under := v.SP - 2
			v.OperandStack[top], v.OperandStack[under] = v.OperandStack[under], v.OperandStack[top]
		}
	}
}
