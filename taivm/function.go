package taivm

type Function struct {
	Name        string
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
