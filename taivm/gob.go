package taivm

import "encoding/gob"

func init() {
	gob.Register(&Function{})
	gob.Register(&Closure{})
	gob.Register(Frame{})
	gob.Register(&Env{})
	gob.Register(OpCode(0))
	gob.Register(Symbol(0))
	gob.Register(NativeFunc{})
	gob.Register([]any{})
	gob.Register(map[any]any{})
	gob.Register(Undefined{})
	gob.Register(&ListIterator{})
	gob.Register(&MapIterator{})
}
