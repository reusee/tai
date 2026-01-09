package taivm

type Function struct {
	Name        string
	Type        Type
	NumParams   int
	ParamNames  []string
	NumLocals   int
	NumDefaults int
	Variadic    bool
	Code        []OpCode
	Constants   []any
}

type BoundMethod struct {
	Receiver any
	Fun      any
}
