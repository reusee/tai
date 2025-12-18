package taivm

type Function struct {
	Name       string
	NumParams  int
	ParamNames []string
	Code       []OpCode
	Constants  []any
}
