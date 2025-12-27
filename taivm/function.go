package taivm

import "reflect"

type Function struct {
	Name        string
	Type        reflect.Type
	NumParams   int
	ParamNames  []string
	NumDefaults int
	Variadic    bool
	Code        []OpCode
	Constants   []any
}

type BoundMethod struct {
	Receiver any
	Fun      any
}
