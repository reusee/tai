package tailang

import (
	"encoding/gob"
	"io"
)

type VMState struct {
	CurrentFun   *Function
	IP           int
	OperandStack []any
	SP           int
	CallStack    []Frame
	Scope        *Env
}

type VM struct {
	State *VMState
}

func NewVM(main *Function) *VM {
	scope := &Env{}
	return &VM{
		State: &VMState{
			CurrentFun:   main,
			Scope:        scope,
			OperandStack: make([]any, 1024),
			CallStack:    make([]Frame, 0, 64),
		},
	}
}

func (v *VM) push(val any) {
	if v.State.SP >= len(v.State.OperandStack) {
		newCap := len(v.State.OperandStack) * 2
		if newCap == 0 {
			newCap = 8
		}
		newStack := make([]any, newCap)
		copy(newStack, v.State.OperandStack)
		v.State.OperandStack = newStack
	}
	v.State.OperandStack[v.State.SP] = val
	v.State.SP++
}

func (v *VM) pop() any {
	if v.State.SP <= 0 {
		return nil
	}
	v.State.SP--
	val := v.State.OperandStack[v.State.SP]
	v.State.OperandStack[v.State.SP] = nil
	return val
}

func (v *VM) drop(n int) {
	if n <= 0 {
		return
	}
	if n > v.State.SP {
		n = v.State.SP
	}
	start := v.State.SP - n
	for i := 0; i < n; i++ {
		v.State.OperandStack[start+i] = nil
	}
	v.State.SP = start
}

func (v *VM) readUint16() uint16 {
	code := v.State.CurrentFun.Code
	if v.State.IP+1 >= len(code) {
		return 0
	}
	hi := uint16(code[v.State.IP])
	lo := uint16(code[v.State.IP+1])
	v.State.IP += 2
	return hi<<8 | lo
}

func (v *VM) Suspend(w io.Writer) error {
	enc := gob.NewEncoder(w)

	syms := SnapshotSymbols()
	if err := enc.Encode(syms); err != nil {
		return err
	}

	if err := enc.Encode(v.State); err != nil {
		return err
	}

	return nil
}

func (v *VM) Restore(r io.Reader) error {
	dec := gob.NewDecoder(r)

	var syms []string
	if err := dec.Decode(&syms); err != nil {
		return err
	}
	for _, s := range syms {
		Intern(s)
	}

	var state VMState
	if err := dec.Decode(&state); err != nil {
		return err
	}
	v.State = &state
	return nil
}
