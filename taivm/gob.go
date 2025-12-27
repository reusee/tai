package taivm

import "encoding/gob"

func init() {
	gob.Register(&Function{})
	gob.Register(&Closure{})
	gob.Register(Frame{})
	gob.Register(&Env{})
	gob.Register(EnvVar{})
	gob.Register(OpCode(0))
	gob.Register(NativeFunc{})
	gob.Register([]any{})
	gob.Register(map[any]any{})
	gob.Register(map[string]any{})
	gob.Register(&ListIterator{})
	gob.Register(&MapIterator{})
	gob.Register(&List{})
	gob.Register(&Struct{})
	gob.Register(&Pointer{})
	gob.Register(&BoundMethod{})
	gob.Register(&Range{})
	gob.Register(&RangeIterator{})
	gob.Register(&Interrupt{})
}
