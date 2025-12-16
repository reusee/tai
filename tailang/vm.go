package tailang

type VMState struct {
	CurrentFun   *Function
	IP           int
	OperandStack []any
	SP           int
	CallStack    []*Frame
	Scope        *Env
}

type VM struct {
	State *VMState
}

func NewVM(main *Function) *VM {
	// Ensure main has an Env scope
	scope := &Env{Vars: make(map[string]any)}
	return &VM{
		State: &VMState{
			CurrentFun:   main,
			Scope:        scope,
			OperandStack: make([]any, 1024),
			CallStack:    make([]*Frame, 0, 64),
		},
	}
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

func (v *VM) popN(n int) []any {
	if v.State.SP < n {
		return nil
	}
	start := v.State.SP - n
	// Copy slice to avoid memory leaks or reference issues if stack grows/shrinks
	args := make([]any, n)
	copy(args, v.State.OperandStack[start:v.State.SP])

	// Clear stack slots
	for i := start; i < v.State.SP; i++ {
		v.State.OperandStack[i] = nil
	}
	v.State.SP = start
	return args
}

func (v *VM) readUint16() uint16 {
	code := v.State.CurrentFun.Code
	hi := uint16(code[v.State.IP])
	lo := uint16(code[v.State.IP+1])
	v.State.IP += 2
	return hi<<8 | lo
}
