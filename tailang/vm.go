package tailang

type OpCode byte

type VMState struct {
	IP           int
	OperandStack []any
	CallStack    []*Frame
	GlobalEnv    *Environment
}

type Frame struct {
	ReturnIP int
	Env      *Environment
}

type Environment struct {
	Parent *Environment
	Vars   map[string]any
}

type VM struct {
	Code      []OpCode
	Constants []any
	State     *VMState
}
