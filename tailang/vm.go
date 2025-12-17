package tailang

import (
	"encoding/gob"
	"io"
)

type VM struct {
	CurrentFun   *Function
	IP           int
	OperandStack []any
	SP           int
	CallStack    []Frame
	Scope        *Env
}

func NewVM(main *Function) *VM {
	scope := &Env{}
	return &VM{
		CurrentFun:   main,
		Scope:        scope,
		OperandStack: make([]any, 1024),
		CallStack:    make([]Frame, 0, 64),
	}
}

func (v *VM) push(val any) {
	if v.SP >= len(v.OperandStack) {
		newCap := len(v.OperandStack) * 2
		if newCap == 0 {
			newCap = 8
		}
		newStack := make([]any, newCap)
		copy(newStack, v.OperandStack)
		v.OperandStack = newStack
	}
	v.OperandStack[v.SP] = val
	v.SP++
}

func (v *VM) pop() any {
	if v.SP <= 0 {
		return nil
	}
	v.SP--
	val := v.OperandStack[v.SP]
	v.OperandStack[v.SP] = nil
	return val
}

func (v *VM) drop(n int) {
	if n <= 0 {
		return
	}
	if n > v.SP {
		n = v.SP
	}
	start := v.SP - n
	for i := 0; i < n; i++ {
		v.OperandStack[start+i] = nil
	}
	v.SP = start
}

func (v *VM) readUint16() uint16 {
	code := v.CurrentFun.Code
	if v.IP+1 >= len(code) {
		return 0
	}
	hi := uint16(code[v.IP])
	lo := uint16(code[v.IP+1])
	v.IP += 2
	return hi<<8 | lo
}

func (v *VM) Snapshot(w io.Writer) error {
	enc := gob.NewEncoder(w)

	syms := SnapshotSymbols()
	if err := enc.Encode(syms); err != nil {
		return err
	}

	if err := enc.Encode(v); err != nil {
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

	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}
