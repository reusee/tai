package tailang

type VMState struct {
	IP           int
	OperandStack []any
	SP           int
	CallStack    []*Frame
	Scope        *Env
}

type VM struct {
	Code      []OpCode
	Constants []any
	State     *VMState
}

func (v *VM) push(val any) {
	if v.State.SP >= len(v.State.OperandStack) {
		v.State.OperandStack = append(v.State.OperandStack, val)
	} else {
		v.State.OperandStack[v.State.SP] = val
	}
	v.State.SP++
}

func (v *VM) pop() any {
	if v.State.SP <= 0 {
		return nil
	}
	v.State.SP--
	val := v.State.OperandStack[v.State.SP]
	v.State.OperandStack[v.State.SP] = nil // Help GC
	return val
}

func (v *VM) readUint16() uint16 {
	hi := uint16(v.Code[v.State.IP])
	lo := uint16(v.Code[v.State.IP+1])
	v.State.IP += 2
	return hi<<8 | lo
}
