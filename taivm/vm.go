package taivm

import (
	"bytes"
	"encoding/gob"
	"io"
	"reflect"
)

var (
	errorType = reflect.TypeFor[error]()
)

type VM struct {
	MainFun      *Function
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
		MainFun:      main,
		CurrentFun:   main,
		Scope:        scope,
		OperandStack: make([]any, 1024),
		CallStack:    make([]Frame, 0, 64),
	}
}

var _ gob.GobEncoder = (*VM)(nil)
var _ gob.GobDecoder = (*VM)(nil)

func (v *VM) GobEncode() ([]byte, error) {
	// Use a proxy struct to control which fields are serialized
	type snapshot struct {
		MainFun      *Function
		CurrentFun   *Function
		IP           int
		OperandStack []any
		SP           int
		BP           int
		CallStack    []Frame
		Scope        *Env
		IsPanicking  bool
		PanicValue   any
		Defers       []*Closure
	}
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(snapshot{
		MainFun:      v.MainFun,
		CurrentFun:   v.CurrentFun,
		IP:           v.IP,
		OperandStack: v.OperandStack[:v.SP], // Only serialize used stack
		SP:           v.SP,
		BP:           v.BP,
		CallStack:    v.CallStack,
		Scope:        v.Scope,
		IsPanicking:  v.IsPanicking,
		PanicValue:   v.PanicValue,
		Defers:       v.Defers,
	})
	return buf.Bytes(), err
}

func (v *VM) GobDecode(data []byte) error {
	type snapshot struct {
		MainFun      *Function
		CurrentFun   *Function
		IP           int
		OperandStack []any
		SP           int
		BP           int
		CallStack    []Frame
		Scope        *Env
		IsPanicking  bool
		PanicValue   any
		Defers       []*Closure
	}
	dec := gob.NewDecoder(bytes.NewReader(data))
	var s snapshot
	if err := dec.Decode(&s); err != nil {
		return err
	}
	v.MainFun = s.MainFun
	v.CurrentFun = s.CurrentFun
	v.IP = s.IP
	v.SP = s.SP
	v.BP = s.BP
	v.CallStack = s.CallStack
	v.Scope = s.Scope
	v.IsPanicking = s.IsPanicking
	v.PanicValue = s.PanicValue
	v.Defers = s.Defers
	// Restore OperandStack with capacity
	v.OperandStack = make([]any, max(1024, v.SP))
	copy(v.OperandStack, s.OperandStack)
	return nil
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
	v.CurrentFun = v.MainFun
	v.IP = 0
	v.SP = 0
	v.BP = 0
	clear(v.OperandStack)
	v.CallStack = v.CallStack[:0]
	v.Defers = nil // Clear current defers
	v.freeEnv(v.Scope)
	v.Scope = v.allocEnv(nil)
}

func (v *VM) handleNativeReturn(out []reflect.Value) (any, error) {
	if len(out) == 0 {
		return nil, nil
	}
	wrap := func(rv reflect.Value) any {
		val := rv.Interface()
		if rt, ok := val.(reflect.Type); ok {
			return FromReflectType(rt)
		}
		return val
	}
	last := out[len(out)-1]
	if last.Type().Implements(errorType) {
		var err error
		if !last.IsNil() {
			err = last.Interface().(error)
		}
		if len(out) == 1 {
			return nil, err
		}
		if len(out) == 2 {
			return wrap(out[0]), err
		}
		res := make([]any, len(out)-1)
		for i := 0; i < len(out)-1; i++ {
			res[i] = wrap(out[i])
		}
		return &List{Elements: res, Immutable: true}, err
	}
	if len(out) == 1 {
		return wrap(out[0]), nil
	}
	res := make([]any, len(out))
	for i, val := range out {
		res[i] = wrap(val)
	}
	return &List{Elements: res, Immutable: true}, nil
}
