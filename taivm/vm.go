package taivm

import (
	"encoding/gob"
	"io"
)

type VM struct {
	mainFun      *Function
	CurrentFun   *Function
	IP           int
	OperandStack []any
	SP           int
	BP           int
	CallStack    []Frame
	Scope        *Env
	envPool      []*Env
	IsPanicking  bool       // whether currently unwinding from a panic
	PanicValue   any        // value passed to panic()
	Defers       []*Closure // stack of deferred functions for the current function
}

func NewVM(main *Function) *VM {
	scope := &Env{}
	return &VM{
		mainFun:      main,
		CurrentFun:   main,
		Scope:        scope,
		OperandStack: make([]any, 1024),
		CallStack:    make([]Frame, 0, 64),
	}
}

func (v *VM) Get(name string) (any, bool) {
	return v.Scope.Get(name)
}

func (v *VM) Def(name string, val any) {
	v.Scope.Def(name, val)
}

func (v *VM) Set(name string, val any) bool {
	return v.Scope.Set(name, val)
}

func (v *VM) push(val any) {
	if v.SP >= len(v.OperandStack) {
		v.growOperandStack()
	}
	v.OperandStack[v.SP] = val
	v.SP++
}

func (v *VM) growOperandStack() {
	newCap := len(v.OperandStack) * 2
	if newCap == 0 {
		newCap = 8
	}
	newStack := make([]any, newCap)
	copy(newStack, v.OperandStack)
	v.OperandStack = newStack
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

func (v *VM) Snapshot(w io.Writer) error {
	enc := gob.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return nil
}

func (v *VM) Restore(r io.Reader) error {
	dec := gob.NewDecoder(r)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func (v *VM) allocEnv(parent *Env) *Env {
	if n := len(v.envPool); n > 0 {
		e := v.envPool[n-1]
		v.envPool = v.envPool[:n-1]
		e.Parent = parent
		e.Vars = e.Vars[:0]
		e.Captured = false
		return e
	}
	return &Env{
		Parent: parent,
	}
}

func (v *VM) freeEnv(e *Env) {
	if e == nil || e.Captured {
		return
	}
	clear(e.Vars)
	v.envPool = append(v.envPool, e)
}

func (v *VM) Reset() {
	v.CurrentFun = v.mainFun
	v.IP = 0
	v.SP = 0
	v.BP = 0
	clear(v.OperandStack)
	v.CallStack = v.CallStack[:0]
	v.Defers = nil // Clear current defers
	v.freeEnv(v.Scope)
	v.Scope = v.allocEnv(nil)
}
