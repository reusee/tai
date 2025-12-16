package tailang

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
)
