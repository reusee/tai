package taivm

type Function struct {
	Name       string
	NumParams  int
	ParamNames []string
	Variadic   bool
	Code       []OpCode
	Constants  []any
}
