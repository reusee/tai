package taivm

type OpCode uint32

const (
	OpLoadConst OpCode = iota + 8
	OpLoadVar
	OpDefVar
	OpSetVar
	OpPop
	OpJump
	OpJumpFalse
	OpCall
	OpReturn
	OpSuspend
	OpEnterScope
	OpLeaveScope
	OpMakeClosure
	OpMakeList
	OpMakeMap
	OpGetIndex
	OpSetIndex
	OpSwap
	OpGetLocal
	OpSetLocal
	OpDumpTrace
)

func (o OpCode) With(arg int) OpCode {
	return o | (OpCode(arg) << 8)
}
