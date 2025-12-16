package tailang

import "fmt"

func (v *VM) Run(
	yield func(*Interrupt, error) bool,
) {

	for {

		if v.State.IP >= len(v.Code) {
			return
		}

		op := v.Code[v.State.IP]
		v.State.IP++

		switch op {

		case OpLoadConst:
			idx := v.readUint16()
			v.push(v.Constants[idx])

		case OpLoadVar:
			idx := v.readUint16()
			name := v.Constants[idx].(string)
			val, ok := v.State.Scope.Get(name)
			if !ok {
				yield(nil, fmt.Errorf("undefined variable: %s", name))
				return
			}
			v.push(val)

		case OpDefVar:
			idx := v.readUint16()
			name := v.Constants[idx].(string)
			val := v.pop()
			v.State.Scope.Def(name, val)

		case OpSetVar:
			idx := v.readUint16()
			name := v.Constants[idx].(string)
			val := v.pop()
			if !v.State.Scope.Set(name, val) {
				if !yield(nil, fmt.Errorf("variable not found: %s", name)) {
					return
				}
				return
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

		case OpCall:
			callee := v.pop()
			switch fn := callee.(type) {
			case *Closure:
				v.State.CallStack = append(v.State.CallStack, &Frame{
					ReturnIP: v.State.IP,
					Env:      v.State.Scope,
				})
				v.State.IP = fn.EntryIP
				v.State.Scope = fn.Env.NewChild()
			default:
				yield(nil, fmt.Errorf("not callable: %T", callee))
				return
			}

		case OpReturn:
			val := v.pop()
			n := len(v.State.CallStack)
			if n == 0 {
				return
			}
			frame := v.State.CallStack[n-1]
			v.State.CallStack = v.State.CallStack[:n-1]
			v.State.IP = frame.ReturnIP
			v.State.Scope = frame.Env
			v.push(val)

		case OpSuspend:
			yield(InterruptSuspend, nil)
			return

		case OpEnterScope:
			v.State.Scope = v.State.Scope.NewChild()

		case OpLeaveScope:
			if v.State.Scope.Parent != nil {
				v.State.Scope = v.State.Scope.Parent
			}

		}
	}
}
