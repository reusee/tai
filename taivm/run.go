package taivm

import "fmt"

func (v *VM) Run(yield func(*Interrupt, error) bool) {
	for {
		if v.IP < 0 || v.IP >= len(v.CurrentFun.Code) {
			return
		}

		inst := v.CurrentFun.Code[v.IP]
		v.IP++
		op := inst & 0xff

		switch op {
		case OpLoadConst:
			idx := int(inst >> 8)
			if v.SP >= len(v.OperandStack) {
				v.growOperandStack()
			}
			v.OperandStack[v.SP] = v.CurrentFun.Constants[idx]
			v.SP++

		case OpLoadVar:
			idx := int(inst >> 8)
			c := v.CurrentFun.Constants[idx]
			var sym Symbol
			if s, ok := c.(Symbol); ok {
				sym = s
			} else {
				sym = v.Intern(c.(string))
			}
			val, ok := v.Scope.GetSym(sym)
			if !ok {
				name := v.Symbols.SymToStr[sym]
				if !yield(nil, fmt.Errorf("undefined variable: %s", name)) {
					return
				}
				v.push(nil)
				continue
			}
			v.push(val)

		case OpDefVar:
			idx := int(inst >> 8)
			c := v.CurrentFun.Constants[idx]
			var sym Symbol
			if s, ok := c.(Symbol); ok {
				sym = s
			} else {
				sym = v.Intern(c.(string))
			}
			v.Scope.DefSym(sym, v.pop())

		case OpSetVar:
			idx := int(inst >> 8)
			c := v.CurrentFun.Constants[idx]
			var sym Symbol
			if s, ok := c.(Symbol); ok {
				sym = s
			} else {
				sym = v.Intern(c.(string))
			}
			val := v.pop()
			if !v.Scope.SetSym(sym, val) {
				name := v.Symbols.SymToStr[sym]
				if !yield(nil, fmt.Errorf("variable not found: %s", name)) {
					return
				}
			}

		case OpPop:
			if v.SP > 0 {
				v.SP--
				v.OperandStack[v.SP] = nil
			}

		case OpJump:
			offset := int(int32(inst) >> 8)
			v.IP += offset

		case OpJumpFalse:
			offset := int(int32(inst) >> 8)
			var val any
			if v.SP > 0 {
				v.SP--
				val = v.OperandStack[v.SP]
				v.OperandStack[v.SP] = nil
			}
			var jump bool
			switch x := val.(type) {
			case nil:
				jump = true
			case bool:
				jump = !x
			case int:
				jump = x == 0
			case string:
				jump = x == ""
			}
			if jump {
				v.IP += offset
			}

		case OpMakeClosure:
			idx := int(inst >> 8)
			fun := v.CurrentFun.Constants[idx].(*Function)
			paramSyms := make([]Symbol, len(fun.ParamNames))
			var maxSym int
			for i, name := range fun.ParamNames {
				sym := v.Intern(name)
				paramSyms[i] = sym
				if int(sym) > maxSym {
					maxSym = int(sym)
				}
			}
			v.push(&Closure{
				Fun:         fun,
				Env:         v.Scope,
				ParamSyms:   paramSyms,
				MaxParamSym: maxSym,
			})

		case OpCall:
			argc := int(inst >> 8)
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
				numParams := fn.Fun.NumParams
				if fn.Fun.Variadic {
					if argc < numParams-1 {
						if !yield(nil, fmt.Errorf("arity mismatch: want at least %d, got %d", numParams-1, argc)) {
							return
						}
						v.drop(argc + 1)
						v.push(nil)
						continue
					}

					fixed := numParams - 1
					varArgsCount := argc - fixed
					slice := make([]any, varArgsCount)
					base := calleeIdx + 1 + fixed
					copy(slice, v.OperandStack[base:base+varArgsCount])

					if varArgsCount == 0 {
						v.push(slice)
					} else {
						v.OperandStack[base] = slice
						for i := base + 1; i < v.SP; i++ {
							v.OperandStack[i] = nil
						}
						v.SP = base + 1
					}
					argc = numParams

				} else if argc != numParams {
					if !yield(nil, fmt.Errorf("arity mismatch: want %d, got %d", numParams, argc)) {
						return
					}
					v.drop(argc + 1)
					v.push(nil)
					continue
				}

				newEnv := fn.Env.NewChild()

				paramSyms := fn.ParamSyms
				maxSym := fn.MaxParamSym
				if paramSyms == nil && len(fn.Fun.ParamNames) > 0 {
					paramSyms = make([]Symbol, len(fn.Fun.ParamNames))
					for i, name := range fn.Fun.ParamNames {
						sym := v.Intern(name)
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

				// Bind arguments from stack directly to new environment
				for i := range argc {
					newEnv.DefSym(paramSyms[i], v.OperandStack[calleeIdx+1+i])
				}

				// Tail Call Optimization
				if v.IP < len(v.CurrentFun.Code) && (v.CurrentFun.Code[v.IP]&0xff) == OpReturn {
					dst := v.BP
					if dst > 0 {
						dst--
					}
					src := calleeIdx
					count := argc + 1
					copy(v.OperandStack[dst:], v.OperandStack[src:src+count])

					// Nil out use locations to avoid leakage
					startClean := dst + count
					endClean := v.SP
					clear(v.OperandStack[startClean:endClean])

					v.SP = startClean
					v.BP = dst + 1
				} else {
					v.CallStack = append(v.CallStack, Frame{
						Fun:      v.CurrentFun,
						ReturnIP: v.IP,
						Env:      v.Scope,
						BaseSP:   calleeIdx,
						BP:       v.BP,
					})
					v.BP = calleeIdx + 1
				}

				v.CurrentFun = fn.Fun
				v.IP = 0
				v.Scope = newEnv

			case NativeFunc:
				// Zero-allocation slice view of arguments
				// Note: Slice is valid only until next Stack modification, which is fine for sync calls
				args := v.OperandStack[calleeIdx+1 : v.SP]
				res, err := fn.Func(v, args)

				if err != nil {
					if !yield(nil, err) {
						return
					}
					res = nil
				}
				v.OperandStack[calleeIdx] = res
				for i := calleeIdx + 1; i < v.SP; i++ {
					v.OperandStack[i] = nil
				}
				v.SP = calleeIdx + 1

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
				if v.BP > 0 {
					v.drop(v.SP - (v.BP - 1))
				} else {
					v.drop(v.SP)
				}
				v.push(retVal)
				return
			}
			frame := v.CallStack[n-1]
			v.CallStack = v.CallStack[:n-1]

			// Restore Call Frame
			v.CurrentFun = frame.Fun
			v.IP = frame.ReturnIP
			v.Scope = frame.Env
			v.BP = frame.BP
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
			n := int(inst >> 8)
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
			n := int(inst >> 8)
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

		case OpGetLocal:
			idx := int(inst >> 8)
			if v.SP >= len(v.OperandStack) {
				v.growOperandStack()
			}
			v.OperandStack[v.SP] = v.OperandStack[v.BP+idx]
			v.SP++

		case OpSetLocal:
			idx := int(inst >> 8)
			var val any
			if v.SP > 0 {
				v.SP--
				val = v.OperandStack[v.SP]
				v.OperandStack[v.SP] = nil
			}
			v.OperandStack[v.BP+idx] = val

		case OpDumpTrace:
			var msg string
			for _, frame := range v.CallStack {
				msg += fmt.Sprintf("%s:%d\n", frame.Fun.Name, frame.ReturnIP)
			}
			msg += fmt.Sprintf("%s:%d", v.CurrentFun.Name, v.IP-1)
			if !yield(nil, fmt.Errorf("%s", msg)) {
				return
			}

		case OpBitAnd, OpBitOr, OpBitXor, OpBitLsh, OpBitRsh:
			if v.SP < 2 {
				if !yield(nil, fmt.Errorf("stack underflow during bitwise op")) {
					return
				}
				continue
			}
			b := v.pop()
			a := v.pop()
			i1, ok1 := a.(int)
			i2, ok2 := b.(int)
			if !ok1 || !ok2 {
				if !yield(nil, fmt.Errorf("bitwise operands must be int, got %T and %T", a, b)) {
					return
				}
				v.push(nil)
				continue
			}
			var res int
			switch op {
			case OpBitAnd:
				res = i1 & i2
			case OpBitOr:
				res = i1 | i2
			case OpBitXor:
				res = i1 ^ i2
			case OpBitLsh:
				if i2 < 0 {
					if !yield(nil, fmt.Errorf("negative shift count: %d", i2)) {
						return
					}
					v.push(nil)
					continue
				}
				res = i1 << i2
			case OpBitRsh:
				if i2 < 0 {
					if !yield(nil, fmt.Errorf("negative shift count: %d", i2)) {
						return
					}
					v.push(nil)
					continue
				}
				res = i1 >> i2
			}
			v.push(res)

		case OpBitNot:
			if v.SP < 1 {
				if !yield(nil, fmt.Errorf("stack underflow during bitwise not")) {
					return
				}
				continue
			}
			a := v.pop()
			i, ok := a.(int)
			if !ok {
				if !yield(nil, fmt.Errorf("bitwise not operand must be int, got %T", a)) {
					return
				}
				v.push(nil)
				continue
			}
			v.push(^i)

		case OpAdd, OpSub, OpMul, OpDiv, OpMod:
			if v.SP < 2 {
				if !yield(nil, fmt.Errorf("stack underflow during math op")) {
					return
				}
				continue
			}
			b := v.pop()
			a := v.pop()

			if op == OpAdd {
				s1, ok1 := a.(string)
				s2, ok2 := b.(string)
				if ok1 && ok2 {
					v.push(s1 + s2)
					continue
				}
			}

			i1, ok1 := a.(int)
			i2, ok2 := b.(int)
			if !ok1 || !ok2 {
				if !yield(nil, fmt.Errorf("math operands must be int, got %T and %T", a, b)) {
					return
				}
				v.push(nil)
				continue
			}

			var res int
			switch op {
			case OpAdd:
				res = i1 + i2
			case OpSub:
				res = i1 - i2
			case OpMul:
				res = i1 * i2
			case OpDiv:
				if i2 == 0 {
					if !yield(nil, fmt.Errorf("division by zero")) {
						return
					}
					v.push(nil)
					continue
				}
				res = i1 / i2
			case OpMod:
				if i2 == 0 {
					if !yield(nil, fmt.Errorf("division by zero")) {
						return
					}
					v.push(nil)
					continue
				}
				res = i1 % i2
			}
			v.push(res)

		case OpEq, OpNe:
			if v.SP < 2 {
				if !yield(nil, fmt.Errorf("stack underflow during comparison")) {
					return
				}
				continue
			}
			b := v.pop()
			a := v.pop()
			match := a == b
			if op == OpEq {
				v.push(match)
			} else {
				v.push(!match)
			}

		case OpLt, OpLe, OpGt, OpGe:
			if v.SP < 2 {
				if !yield(nil, fmt.Errorf("stack underflow during comparison")) {
					return
				}
				continue
			}
			b := v.pop()
			a := v.pop()

			var res bool
			switch x := a.(type) {
			case int:
				y, ok := b.(int)
				if !ok {
					if !yield(nil, fmt.Errorf("comparison type mismatch: int vs %T", b)) {
						return
					}
					v.push(nil)
					continue
				}
				switch op {
				case OpLt:
					res = x < y
				case OpLe:
					res = x <= y
				case OpGt:
					res = x > y
				case OpGe:
					res = x >= y
				}
			case string:
				y, ok := b.(string)
				if !ok {
					if !yield(nil, fmt.Errorf("comparison type mismatch: string vs %T", b)) {
						return
					}
					v.push(nil)
					continue
				}
				switch op {
				case OpLt:
					res = x < y
				case OpLe:
					res = x <= y
				case OpGt:
					res = x > y
				case OpGe:
					res = x >= y
				}
			default:
				if !yield(nil, fmt.Errorf("unsupported type for comparison: %T", a)) {
					return
				}
				v.push(nil)
				continue
			}
			v.push(res)

		case OpNot:
			if v.SP < 1 {
				if !yield(nil, fmt.Errorf("stack underflow during not")) {
					return
				}
				continue
			}
			val := v.pop()
			var isFalse bool
			switch x := val.(type) {
			case nil:
				isFalse = true
			case bool:
				isFalse = !x
			case int:
				isFalse = x == 0
			case string:
				isFalse = x == ""
			}
			v.push(isFalse)
		}
	}
}
