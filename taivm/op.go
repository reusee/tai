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
	OpMakeStruct
	OpMakeMap
	OpGetIndex
	OpSetIndex
	OpSwap
	OpGetLocal
	OpSetLocal
	OpDumpTrace
	OpBitAnd
	OpBitOr
	OpBitXor
	OpBitNot
	OpBitLsh
	OpBitRsh
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpNot
	OpGetIter
	OpNextIter
	OpMakeTuple
	OpGetSlice
	OpSetSlice
	OpDup
	OpDup2
	OpGetAttr
	OpSetAttr
	OpCallKw
	OpListAppend
	OpContains
	OpFloorDiv
	OpUnpack
	OpPow
	OpImport
	OpDefer
	OpAddrOf
	OpAddrOfIndex
	OpAddrOfAttr
	OpDeref
	OpSetDeref
	OpTypeAssert
	OpTypeAssertOk
)

func (o OpCode) With(arg int) OpCode {
	return o | (OpCode(arg) << 8)
}
