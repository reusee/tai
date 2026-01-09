package taivm

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strings"
)

var ErrYield = errors.New("yield")

func (v *VM) Run(yield func(*Interrupt, error) bool) {
	yieldTicks := v.YieldTicks
	ticks := 0

	for {
		if v.IP < 0 || v.IP >= len(v.CurrentFun.Code) {
			if (v.IsPanicking || len(v.CallStack) > 0) && v.handleUnwind(yield) {
				continue
			}
			return
		}

		if !v.executeOne(yield) {
			return
		}

		if yieldTicks > 0 {
			ticks++
			if ticks >= yieldTicks {
				if !yield(InterruptYield, nil) {
					return
				}
				ticks = 0
			}
		}

	}
}

func (v *VM) executeOne(yield func(*Interrupt, error) bool) bool {
	inst := v.CurrentFun.Code[v.IP]
	v.IP++
	op := inst & 0xff
	switch op {
	case OpLoadConst:
		v.push(v.CurrentFun.Constants[int(inst>>8)])
	case OpGetLocal:
		v.push(v.OperandStack[v.BP+int(inst>>8)])
	case OpSetLocal:
		if v.SP > 0 {
			v.OperandStack[v.BP+int(inst>>8)] = v.pop()
		}
	case OpPop:
		v.pop()
	case OpJump:
		v.IP += int(int32(inst) >> 8)
	case OpJumpFalse:
		if isZero(v.pop()) {
			v.IP += int(int32(inst) >> 8)
		}
	case OpAdd, OpSub:
		if !v.opArithmeticFast(op) && !v.opMath(op, yield) {
			return false
		}
	case OpEq, OpLt:
		if !v.opCompareFast(op) && !v.opCompare(op, yield) {
			return false
		}
	case OpLoadVar:
		return v.opLoadVar(inst, yield)
	case OpDefVar:
		v.opDefVar(inst)
	case OpSetVar:
		return v.opSetVar(inst, yield)
	case OpDup:
		return v.opDup(yield)
	case OpDup2:
		return v.opDup2(yield)
	case OpMakeClosure:
		v.opMakeClosure(inst)
	case OpCall:
		return v.opCall(inst, yield)
	case OpReturn:
		return v.opReturn(yield)
	case OpSuspend:
		return yield(InterruptSuspend, nil)
	case OpEnterScope:
		v.Scope = v.allocEnv(v.Scope)
	case OpLeaveScope:
		v.leaveScope()
	default:
		return v.executeOther(op, inst, yield)
	}
	return true
}

func (v *VM) executeOther(op OpCode, inst OpCode, yield func(*Interrupt, error) bool) bool {
	switch op {
	case OpMakeList:
		return v.opMakeList(inst, yield)
	case OpMakeStruct:
		return v.opMakeStruct(inst, yield)
	case OpMakeMap:
		return v.opMakeMap(inst, yield)
	case OpGetIndex:
		return v.opGetIndex(yield, 0)
	case OpSetIndex:
		return v.opSetIndex(yield, 0)
	case OpSwap:
		return v.opSwap(yield)
	case OpDumpTrace:
		return v.opDumpTrace(yield)
	case OpBitAnd, OpBitOr, OpBitXor, OpBitLsh, OpBitRsh:
		return v.opBitwise(op, yield)
	case OpBitNot:
		return v.opBitNot(yield)
	case OpMul, OpDiv, OpMod, OpFloorDiv, OpPow:
		return v.opMath(op, yield)
	case OpNe, OpLe, OpGt, OpGe:
		return v.opCompare(op, yield)
	case OpNot:
		return v.opNot(yield)
	case OpGetIter:
		return v.opGetIter(yield)
	case OpNextIter:
		return v.opNextIter(inst, yield)
	case OpMakeTuple:
		return v.opMakeTuple(inst, yield)
	case OpGetSlice:
		return v.opGetSlice(yield)
	case OpSetSlice:
		return v.opSetSlice(yield)
	case OpGetAttr:
		return v.opGetAttr(yield, 0)
	case OpSetAttr:
		return v.opSetAttr(yield, 0)
	case OpCallKw:
		return v.opCallKw(inst, yield)
	case OpListAppend:
		return v.opListAppend(yield)
	case OpContains:
		return v.opContains(yield)
	case OpUnpack:
		return v.opUnpack(inst, yield)
	case OpImport:
		return v.opImport(inst, yield)
	case OpDefer:
		v.opDefer()
	case OpAddrOf:
		return v.opAddrOf(inst, yield)
	case OpAddrOfIndex:
		return v.opAddrOfIndex(yield)
	case OpAddrOfAttr:
		return v.opAddrOfAttr(yield)
	case OpDeref:
		return v.opDeref(yield, 0)
	case OpSetDeref:
		return v.opSetDeref(yield, 0)
	case OpTypeAssert:
		return v.opTypeAssert(yield)
	case OpTypeAssertOk:
		return v.opTypeAssertOk(yield)
	case OpGetIndexOk:
		return v.opGetIndexOk(yield, 0)
	}
	return true
}

func (v *VM) opArithmeticFast(op OpCode) bool {
	if v.SP < 2 {
		return false
	}
	b, a := v.OperandStack[v.SP-1], v.OperandStack[v.SP-2]
	if x, ok := a.(int); ok {
		if y, ok := b.(int); ok {
			if op == OpAdd {
				v.OperandStack[v.SP-2] = x + y
			} else {
				v.OperandStack[v.SP-2] = x - y
			}
			v.SP--
			v.OperandStack[v.SP] = nil
			return true
		}
	}
	if x, ok := a.(int64); ok {
		if y, ok := b.(int64); ok {
			if op == OpAdd {
				v.OperandStack[v.SP-2] = x + y
			} else {
				v.OperandStack[v.SP-2] = x - y
			}
			v.SP--
			v.OperandStack[v.SP] = nil
			return true
		}
	}
	if x, ok := a.(float64); ok {
		if y, ok := b.(float64); ok {
			if op == OpAdd {
				v.OperandStack[v.SP-2] = x + y
			} else {
				v.OperandStack[v.SP-2] = x - y
			}
			v.SP--
			v.OperandStack[v.SP] = nil
			return true
		}
	}
	return false
}

func (v *VM) opCompareFast(op OpCode) bool {
	if v.SP < 2 {
		return false
	}
	b, a := v.OperandStack[v.SP-1], v.OperandStack[v.SP-2]
	if x, ok := a.(int); ok {
		if y, ok := b.(int); ok {
			var res bool
			switch op {
			case OpEq:
				res = x == y
			case OpNe:
				res = x != y
			case OpLt:
				res = x < y
			case OpLe:
				res = x <= y
			case OpGt:
				res = x > y
			case OpGe:
				res = x >= y
			default:
				return false
			}
			v.OperandStack[v.SP-2] = res
			v.SP--
			v.OperandStack[v.SP] = nil
			return true
		}
	}
	if x, ok := a.(int64); ok {
		if y, ok := b.(int64); ok {
			var res bool
			switch op {
			case OpEq:
				res = x == y
			case OpNe:
				res = x != y
			case OpLt:
				res = x < y
			case OpLe:
				res = x <= y
			case OpGt:
				res = x > y
			case OpGe:
				res = x >= y
			default:
				return false
			}
			v.OperandStack[v.SP-2] = res
			v.SP--
			v.OperandStack[v.SP] = nil
			return true
		}
	}
	if x, ok := a.(float64); ok {
		if y, ok := b.(float64); ok {
			var res bool
			switch op {
			case OpEq:
				res = x == y
			case OpNe:
				res = x != y
			case OpLt:
				res = x < y
			case OpLe:
				res = x <= y
			case OpGt:
				res = x > y
			case OpGe:
				res = x >= y
			default:
				return false
			}
			v.OperandStack[v.SP-2] = res
			v.SP--
			v.OperandStack[v.SP] = nil
			return true
		}
	}
	return false
}

func (v *VM) leaveScope() {
	if v.Scope.Parent != nil {
		old := v.Scope
		v.Scope = v.Scope.Parent
		v.freeEnv(old)
	}
}

func ToInt64(v any) (int64, bool) {
	switch i := v.(type) {
	case int:
		return int64(i), true
	case int64:
		return i, true
	case *big.Int:
		return i.Int64(), i.IsInt64()
	case nil:
		return 0, true
	case uint8:
		return int64(i), true
	case int32:
		return int64(i), true
	case uint32:
		return int64(i), true
	case int8:
		return int64(i), true
	case int16:
		return int64(i), true
	case uint:
		return int64(i), true
	case uint16:
		return int64(i), true
	case uint64:
		return int64(i), i <= math.MaxInt64
	}
	return 0, false
}

func ToIntBig(v any) (*big.Int, bool) {
	switch i := v.(type) {
	case *big.Int:
		return i, true
	case int:
		return big.NewInt(int64(i)), true
	case int64:
		return big.NewInt(i), true
	case uint64:
		return new(big.Int).SetUint64(i), true
	}
	if i, ok := ToInt64(v); ok {
		return big.NewInt(i), true
	}
	return nil, false
}

func ToFloat64(v any) (float64, bool) {
	switch i := v.(type) {
	case float64:
		return i, true
	case *big.Float:
		f, _ := i.Float64()
		return f, true
	case *big.Int:
		f, _ := new(big.Float).SetInt(i).Float64()
		return f, true
	case int:
		return float64(i), true
	case int64:
		return float64(i), true
	case nil:
		return 0, true
	case float32:
		return float64(i), true
	case int8:
		return float64(i), true
	case int16:
		return float64(i), true
	case int32:
		return float64(i), true
	case uint:
		return float64(i), true
	case uint8:
		return float64(i), true
	case uint16:
		return float64(i), true
	case uint32:
		return float64(i), true
	case uint64:
		return float64(i), true
	}
	return 0, false
}

func ToFloatBig(v any) (*big.Float, bool) {
	switch i := v.(type) {
	case *big.Float:
		return i, true
	case *big.Int:
		return new(big.Float).SetInt(i), true
	case float64:
		return big.NewFloat(i), true
	}
	if f, ok := ToFloat64(v); ok {
		return big.NewFloat(f), true
	}
	return nil, false
}

func toComplex128(v any) (complex128, bool) {
	switch i := v.(type) {
	case complex128:
		return i, true
	case complex64:
		return complex128(i), true
	}
	if f, ok := ToFloat64(v); ok {
		return complex(f, 0), true
	}
	return 0, false
}

func isComplex(v any) bool {
	switch v.(type) {
	case complex64, complex128:
		return true
	}
	return false
}

func isZero(v any) bool {
	switch i := v.(type) {
	case bool:
		return !i
	case int:
		return i == 0
	case int8:
		return i == 0
	case int16:
		return i == 0
	case int32:
		return i == 0
	case int64:
		return i == 0
	case uint:
		return i == 0
	case uint8:
		return i == 0
	case uint16:
		return i == 0
	case uint32:
		return i == 0
	case uint64:
		return i == 0
	case *big.Int:
		return i.Sign() == 0
	case *big.Float:
		return i.Sign() == 0
	case string:
		return i == ""
	case nil:
		return true
	case float32:
		return i == 0
	case float64:
		return i == 0
	case complex64:
		return i == 0
	case complex128:
		return i == 0
	}
	if i, ok := ToInt64(v); ok {
		return i == 0
	}
	if f, ok := ToFloat64(v); ok {
		return f == 0
	}
	if c, ok := toComplex128(v); ok {
		return c == 0
	}
	return false
}

func arithmeticSameType(op OpCode, a, b any) (any, bool, error) {
	switch x := a.(type) {
	case int:
		if y, ok := b.(int); ok {
			switch op {
			case OpAdd:
				return x + y, true, nil
			case OpSub:
				return x - y, true, nil
			case OpMul:
				return x * y, true, nil
			case OpDiv:
				if y == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				return x / y, true, nil
			case OpMod:
				if y == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				r := x % y
				if (r < 0) != (y < 0) && r != 0 {
					r += y
				}
				return r, true, nil
			case OpFloorDiv:
				if y == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				q := x / y
				if (x < 0) != (y < 0) && x%y != 0 {
					q--
				}
				return q, true, nil
			}
		}
	case int64:
		if y, ok := b.(int64); ok {
			switch op {
			case OpAdd:
				return x + y, true, nil
			case OpSub:
				return x - y, true, nil
			case OpMul:
				return x * y, true, nil
			case OpDiv:
				if y == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				return x / y, true, nil
			case OpMod:
				if y == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				r := x % y
				if (r < 0) != (y < 0) && r != 0 {
					r += y
				}
				return r, true, nil
			case OpFloorDiv:
				if y == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				q := x / y
				if (x < 0) != (y < 0) && x%y != 0 {
					q--
				}
				return q, true, nil
			}
		}
	case *big.Int:
		if y, ok := b.(*big.Int); ok {
			switch op {
			case OpAdd:
				return new(big.Int).Add(x, y), true, nil
			case OpSub:
				return new(big.Int).Sub(x, y), true, nil
			case OpMul:
				return new(big.Int).Mul(x, y), true, nil
			case OpDiv:
				if y.Sign() == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				return new(big.Int).Quo(x, y), true, nil
			case OpMod:
				if y.Sign() == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				return new(big.Int).Rem(x, y), true, nil
			}
		}
	case float64:
		if y, ok := b.(float64); ok {
			switch op {
			case OpAdd:
				return x + y, true, nil
			case OpSub:
				return x - y, true, nil
			case OpMul:
				return x * y, true, nil
			case OpDiv:
				if y == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				return x / y, true, nil
			case OpFloorDiv:
				if y == 0 {
					return nil, true, fmt.Errorf("division by zero")
				}
				return math.Floor(x / y), true, nil
			}
		}
	case *big.Float:
		if y, ok := b.(*big.Float); ok {
			switch op {
			case OpAdd:
				return new(big.Float).Add(x, y), true, nil
			case OpSub:
				return new(big.Float).Sub(x, y), true, nil
			case OpMul:
				return new(big.Float).Mul(x, y), true, nil
			case OpDiv:
				return new(big.Float).Quo(x, y), true, nil
			}
		}
	case string:
		if y, ok := b.(string); ok && op == OpAdd {
			return x + y, true, nil
		}
	}
	return nil, false, nil
}

func bitwiseSameType(op OpCode, a, b any) (any, bool, error) {
	switch x := a.(type) {
	case int:
		if y, ok := b.(int); ok {
			switch op {
			case OpBitAnd:
				return x & y, true, nil
			case OpBitOr:
				return x | y, true, nil
			case OpBitXor:
				return x ^ y, true, nil
			case OpBitLsh:
				if y < 0 {
					return nil, true, fmt.Errorf("negative shift count: %d", y)
				}
				return x << uint(y), true, nil
			case OpBitRsh:
				if y < 0 {
					return nil, true, fmt.Errorf("negative shift count: %d", y)
				}
				return x >> uint(y), true, nil
			}
		}
	case int64:
		if y, ok := b.(int64); ok {
			switch op {
			case OpBitAnd:
				return x & y, true, nil
			case OpBitOr:
				return x | y, true, nil
			case OpBitXor:
				return x ^ y, true, nil
			case OpBitLsh:
				if y < 0 {
					return nil, true, fmt.Errorf("negative shift count: %d", y)
				}
				return x << uint(y), true, nil
			case OpBitRsh:
				if y < 0 {
					return nil, true, fmt.Errorf("negative shift count: %d", y)
				}
				return x >> uint(y), true, nil
			}
		}
	case *big.Int:
		if y, ok := b.(*big.Int); ok {
			switch op {
			case OpBitAnd:
				return new(big.Int).And(x, y), true, nil
			case OpBitOr:
				return new(big.Int).Or(x, y), true, nil
			case OpBitXor:
				return new(big.Int).Xor(x, y), true, nil
			case OpBitLsh:
				return new(big.Int).Lsh(x, uint(y.Uint64())), true, nil
			case OpBitRsh:
				return new(big.Int).Rsh(x, uint(y.Uint64())), true, nil
			}
		}
	}
	return nil, false, nil
}

func resolveSliceIndices(length int, start, stop, step any) (int, int, int, error) {
	stepInt := 1
	if step != nil {
		s, ok := ToInt64(step)
		if !ok {
			return 0, 0, 0, fmt.Errorf("slice step must be integer")
		}
		stepInt = int(s)
	}
	if stepInt == 0 {
		return 0, 0, 0, fmt.Errorf("slice step cannot be zero")
	}

	// Clamp start
	var startInt int
	if start == nil {
		if stepInt > 0 {
			startInt = 0
		} else {
			startInt = length - 1
		}
	} else {
		s, ok := ToInt64(start)
		if !ok {
			return 0, 0, 0, fmt.Errorf("slice start must be integer")
		}
		startInt = int(s)
		if startInt < 0 {
			startInt += length
		}
	}
	// Bound check start
	if startInt < 0 {
		if stepInt > 0 {
			startInt = 0
		} else {
			startInt = -1
		}
	} else if startInt >= length {
		if stepInt > 0 {
			startInt = length
		} else {
			startInt = length - 1
		}
	}

	// Clamp stop
	var stopInt int
	if stop == nil {
		if stepInt > 0 {
			stopInt = length
		} else {
			stopInt = -1
		}
	} else {
		s, ok := ToInt64(stop)
		if !ok {
			return 0, 0, 0, fmt.Errorf("slice stop must be integer")
		}
		stopInt = int(s)
		if stopInt < 0 {
			stopInt += length
		}
	}
	// Bound check stop
	if stopInt < 0 {
		if stepInt > 0 {
			stopInt = 0
		} else {
			stopInt = -1
		}
	} else if stopInt >= length {
		if stepInt > 0 {
			stopInt = length
		} else {
			stopInt = length - 1
		}
	}

	return startInt, stopInt, stepInt, nil
}

func (v *VM) opLoadVar(inst OpCode, yield func(*Interrupt, error) bool) bool {
	idx := int(inst >> 8)
	name := v.CurrentFun.Constants[idx].(string)
	val, ok := v.Scope.Get(name)
	if !ok {
		// Handle virtual embedding info lookup
		if strings.HasPrefix(name, "_embedded_info_") {
			v.push(&List{})
			return true
		}
		if !yield(nil, fmt.Errorf("undefined variable: %s", name)) {
			return false
		}
		v.push(nil)
		return true
	}
	v.push(val)
	return true
}

func (v *VM) opDefVar(inst OpCode) {
	arg := int(inst >> 8)
	// Lower 23 bits are the constant index for the variable name
	name := v.CurrentFun.Constants[arg&0x7fffff].(string)
	// Bit 23 (the 24th bit of the 24-bit argument) is a flag for typed definition
	if arg&(1<<23) != 0 {
		typ := v.pop().(*Type) // Pop type first
		val := v.pop()         // Then value
		v.DefWithType(name, val, typ)
	} else {
		val := v.pop()
		v.Def(name, val)
	}
}

func (v *VM) opSetVar(inst OpCode, yield func(*Interrupt, error) bool) bool {
	idx := int(inst >> 8)
	name := v.CurrentFun.Constants[idx].(string)
	val := v.pop()
	if !v.Scope.Set(name, val) {
		if !yield(nil, fmt.Errorf("variable not found: %s", name)) {
			return false
		}
	}
	return true
}

func (v *VM) opDup(yield func(*Interrupt, error) bool) bool {
	if v.SP < 1 {
		if !yield(nil, fmt.Errorf("stack underflow during dup")) {
			return false
		}
		return true
	}
	v.push(v.OperandStack[v.SP-1])
	return true
}

func (v *VM) opDup2(yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		if !yield(nil, fmt.Errorf("stack underflow during dup2")) {
			return false
		}
		return true
	}
	b := v.OperandStack[v.SP-1]
	a := v.OperandStack[v.SP-2]
	v.push(a)
	v.push(b)
	return true
}

func (v *VM) opMakeClosure(inst OpCode) {
	idx := int(inst >> 8)
	fun := v.CurrentFun.Constants[idx].(*Function)
	var defaults []any
	if fun.NumDefaults > 0 {
		defaults = make([]any, fun.NumDefaults)
		for i := fun.NumDefaults - 1; i >= 0; i-- {
			defaults[i] = v.pop()
		}
	}

	v.Scope.MarkCaptured()
	v.push(&Closure{
		Fun:      fun,
		Env:      v.Scope,
		Defaults: defaults,
	})
}

func (v *VM) opCall(inst OpCode, yield func(*Interrupt, error) bool) bool {
	argc := int(inst >> 8)
	if v.SP < argc+1 {
		if !yield(nil, fmt.Errorf("stack underflow during call")) {
			return false
		}
		return true
	}

	calleeIdx := v.SP - argc - 1
	callee := v.OperandStack[calleeIdx]

	switch fn := callee.(type) {
	case *Closure:
		return v.callClosure(fn, argc, calleeIdx, yield)

	case *BoundMethod:
		if v.SP >= len(v.OperandStack) {
			v.growOperandStack()
		}

		copy(v.OperandStack[calleeIdx+2:v.SP+1], v.OperandStack[calleeIdx+1:v.SP])
		v.SP++
		v.OperandStack[calleeIdx] = fn.Fun
		v.OperandStack[calleeIdx+1] = fn.Receiver
		return v.opCall(OpCall.With(argc+1), yield)

	case NativeFunc:
		return v.callNative(fn, argc, calleeIdx, yield)

	case *Type:
		return v.callTypeConversion(fn, argc, calleeIdx, yield)

	case reflect.Type:
		return v.callTypeConversion(FromReflectType(fn), argc, calleeIdx, yield)

	default:
		if !yield(nil, fmt.Errorf("calling non-function: %T", callee)) {
			return false
		}
		v.drop(argc + 1)
		v.push(nil)
		return true
	}
}

func (v *VM) callClosure(fn *Closure, argc, calleeIdx int, yield func(*Interrupt, error) bool) bool {
	numParams := fn.Fun.NumParams
	isVariadic := fn.Fun.Variadic
	defaults := fn.Defaults
	numDefaults := len(defaults)

	numFixed := numParams
	if isVariadic {
		numFixed--
	}
	minArgs := numFixed - numDefaults

	if argc < minArgs {
		if !yield(nil, fmt.Errorf("arity mismatch: want at least %d, got %d", minArgs, argc)) {
			return false
		}
		v.drop(argc + 1)
		v.push(nil)
		return true
	}

	if !isVariadic && argc > numFixed {
		if !yield(nil, fmt.Errorf("arity mismatch: want %d, got %d", numFixed, argc)) {
			return false
		}
		v.drop(argc + 1)
		v.push(nil)
		return true
	}

	numLocals := max(fn.Fun.NumLocals, numParams)
	locals := make([]any, numLocals)

	for i := range numFixed {
		if i < argc {
			locals[i] = v.OperandStack[calleeIdx+1+i]
		} else {
			defIdx := i - (numFixed - numDefaults)
			locals[i] = defaults[defIdx]
		}
	}

	if isVariadic {
		var slice []any
		if argc > numFixed {
			count := argc - numFixed
			slice = make([]any, count)
			base := calleeIdx + 1 + numFixed
			copy(slice, v.OperandStack[base:base+count])
		} else {
			slice = []any{}
		}
		locals[numFixed] = &List{
			Elements:  slice,
			Immutable: true,
		}
	}

	newEnv := v.allocEnv(fn.Env)
	for i, name := range fn.Fun.ParamNames {
		if i < len(locals) {
			newEnv.Def(name, locals[i])
		}
	}

	if v.IP < len(v.CurrentFun.Code) && (v.CurrentFun.Code[v.IP]&0xff) == OpReturn && len(v.Defers) == 0 {
		needed := v.BP + len(locals)
		if needed > len(v.OperandStack) {
			newCap := max(len(v.OperandStack)*2, needed)
			newStack := make([]any, newCap)
			copy(newStack, v.OperandStack)
			v.OperandStack = newStack
		}
		copy(v.OperandStack[v.BP:], locals)

		oldSP := v.SP
		v.SP = needed
		if v.SP < oldSP {
			for i := v.SP; i < oldSP; i++ {
				v.OperandStack[i] = nil
			}
		}

		oldScope := v.Scope
		v.CurrentFun = fn.Fun
		v.IP = 0
		v.Scope = newEnv
		v.freeEnv(oldScope)
		return true
	} else {
		v.CallStack = append(v.CallStack, Frame{
			Fun:      v.CurrentFun,
			ReturnIP: v.IP,
			Env:      v.Scope,
			BaseSP:   calleeIdx,
			BP:       v.BP,
			Defers:   v.Defers,
		})

		needed := calleeIdx + 1 + len(locals)
		if needed > len(v.OperandStack) {
			newCap := max(len(v.OperandStack)*2, needed)
			newStack := make([]any, newCap)
			copy(newStack, v.OperandStack)
			v.OperandStack = newStack
		}
		copy(v.OperandStack[calleeIdx+1:], locals)

		oldSP := calleeIdx + 1 + argc
		v.SP = needed
		if v.SP < oldSP {
			for i := v.SP; i < oldSP; i++ {
				v.OperandStack[i] = nil
			}
		}

		v.BP = calleeIdx + 1
		v.Defers = nil // Clear current defers for the new function
	}

	v.CurrentFun = fn.Fun
	v.IP = 0
	v.Scope = newEnv
	return true
}

func (v *VM) callNative(fn NativeFunc, argc, calleeIdx int, yield func(*Interrupt, error) bool) bool {
	args := v.OperandStack[calleeIdx+1 : v.SP]
	res, err := fn.Call(v, args)

	if err != nil {
		if v.IsPanicking {
			v.IP = len(v.CurrentFun.Code)
			return true
		}
		if !yield(nil, err) {
			return false
		}
		res = nil
	}
	if rt, ok := res.(reflect.Type); ok {
		res = FromReflectType(rt)
	}
	v.OperandStack[calleeIdx] = res
	for i := calleeIdx + 1; i < v.SP; i++ {
		v.OperandStack[i] = nil
	}
	v.SP = calleeIdx + 1
	return true
}

func (v *VM) callTypeConversion(t *Type, argc, calleeIdx int, yield func(*Interrupt, error) bool) bool {
	if argc != 1 {
		return yield(nil, fmt.Errorf("type conversion expects 1 argument"))
	}
	arg := v.OperandStack[calleeIdx+1]
	var res any
	if arg == nil {
		res = t.Zero()
	} else if list, ok := arg.(*List); ok && (t.Kind == KindArray || (t.Kind == KindPtr && t.Elem != nil && t.Elem.Kind == KindArray)) {
		if t.Kind == KindArray {
			n := t.Len
			if len(list.Elements) < n {
				v.IsPanicking, v.PanicValue = true, fmt.Sprintf("cannot convert slice with length %d to array of length %d", len(list.Elements), n)
				v.IP = len(v.CurrentFun.Code)
				return true
			}
			rt := t.ToReflectType()
			arr := reflect.New(rt).Elem()
			for i := range n {
				arr.Index(i).Set(reflect.ValueOf(list.Elements[i]))
			}
			res = arr.Interface()
		} else {
			n := t.Elem.Len
			if len(list.Elements) < n {
				v.IsPanicking, v.PanicValue = true, fmt.Sprintf("cannot convert slice with length %d to array pointer of length %d", len(list.Elements), n)
				v.IP = len(v.CurrentFun.Code)
				return true
			}
			res = &Pointer{Target: list, Key: 0, ArrayType: t.Elem}
		}
	} else {
		// Manual scalar conversions
		switch t.Kind {
		case KindInt:
			if i, ok := ToInt64(arg); ok {
				res = int(i)
			}
		case KindInt64:
			if i, ok := ToInt64(arg); ok {
				res = i
			}
		case KindFloat64:
			if f, ok := ToFloat64(arg); ok {
				res = f
			}
		case KindString:
			res = fmt.Sprint(arg)
		}
		// Fallback to reflect for complex native conversions
		if res == nil {
			if rt := t.ToReflectType(); rt != nil {
				val := reflect.ValueOf(arg)
				if val.Type().ConvertibleTo(rt) {
					res = val.Convert(rt).Interface()
				} else {
					return yield(nil, fmt.Errorf("cannot convert %T to %v", arg, rt))
				}
			} else {
				return yield(nil, fmt.Errorf("cannot convert %T to %v", arg, t.Name))
			}
		}
	}
	v.OperandStack[calleeIdx] = res
	v.drop(argc)
	return true
}

func (v *VM) opReturn(yield func(*Interrupt, error) bool) bool {
	// Execute defers if present
	if len(v.Defers) > 0 {
		last := len(v.Defers) - 1
		d := v.Defers[last]
		v.Defers = v.Defers[:last]
		// re-execute return after defer
		v.IP--
		// push closure and call it
		v.push(d)
		return v.opCall(OpCall.With(0), yield)
	}

	retVal := v.pop()
	n := len(v.CallStack)
	if n == 0 {
		if v.BP > 0 {
			v.drop(v.SP - (v.BP - 1))
		} else {
			v.drop(v.SP)
		}
		v.push(retVal)
		v.IP = len(v.CurrentFun.Code)
		return true
	}
	frame := v.CallStack[n-1]
	v.CallStack = v.CallStack[:n-1]

	oldScope := v.Scope
	v.CurrentFun = frame.Fun
	v.IP = frame.ReturnIP
	v.Scope = frame.Env
	v.BP = frame.BP
	v.Defers = frame.Defers // Restore saved defers
	v.drop(v.SP - frame.BaseSP)
	v.push(retVal)
	v.freeEnv(oldScope)
	return true
}

func (v *VM) opMakeList(inst OpCode, yield func(*Interrupt, error) bool) bool {
	n := int(inst >> 8)
	if v.SP < n {
		if !yield(nil, fmt.Errorf("stack underflow during list creation")) {
			return false
		}
		return true
	}
	slice := make([]any, n)
	start := v.SP - n
	copy(slice, v.OperandStack[start:v.SP])
	v.drop(n)
	v.push(&List{Elements: slice, Immutable: false})
	return true
}

func (v *VM) opMakeMap(inst OpCode, yield func(*Interrupt, error) bool) bool {
	n := int(inst >> 8)
	if v.SP < n*2 {
		if !yield(nil, fmt.Errorf("stack underflow during map creation")) {
			return false
		}
		return true
	}
	m := make(map[any]any, n)
	start := v.SP - n*2
	for i := range n {
		k := v.OperandStack[start+i*2]
		val := v.OperandStack[start+i*2+1]
		m[k] = val
	}
	v.drop(n * 2)
	v.push(m)
	return true
}

func (v *VM) opMakeStruct(inst OpCode, yield func(*Interrupt, error) bool) bool {
	n := int(inst >> 8)
	if v.SP < n*2+2 {
		if !yield(nil, fmt.Errorf("stack underflow during struct creation")) {
			return false
		}
		return true
	}

	embObj := v.pop()
	var embedded []string
	if l, ok := embObj.(*List); ok {
		for _, e := range l.Elements {
			if s, ok := e.(string); ok {
				embedded = append(embedded, s)
			}
		}
	}

	typeName := v.pop()
	typeNameStr, ok := typeName.(string)
	if !ok {
		if !yield(nil, fmt.Errorf("struct type name must be string, got %T", typeName)) {
			return false
		}
		v.push(nil)
		return true
	}
	m := make(map[string]any, n)
	start := v.SP - n*2
	for i := range n {
		k := v.OperandStack[start+i*2]
		val := v.OperandStack[start+i*2+1]
		keyStr, ok := k.(string)
		if !ok {
			if !yield(nil, fmt.Errorf("struct field key must be string, got %T", k)) {
				return false
			}
			v.push(nil)
			return true
		}
		m[keyStr] = val
	}
	v.drop(n * 2)

	if tObj, ok := v.Get(typeNameStr); ok {
		var t *Type
		if tt, ok := tObj.(*Type); ok && tt.Kind == KindStruct {
			t = tt
		} else if rt, ok := tObj.(reflect.Type); ok && rt.Kind() == reflect.Struct {
			t = FromReflectType(rt)
		}
		if t != nil {
			for _, f := range t.Fields {
				if f.Anonymous {
					found := false
					for _, e := range embedded {
						if e == f.Name {
							found = true
							break
						}
					}
					if !found {
						embedded = append(embedded, f.Name)
					}
				}
				if _, ok := m[f.Name]; !ok {
					m[f.Name] = f.Type.Zero()
				}
			}
		}
	}

	v.push(&Struct{TypeName: typeNameStr, Fields: m, Embedded: embedded})
	return true
}

func (v *VM) opGetIndex(yield func(*Interrupt, error) bool, depth int) bool {
	if depth > 100 {
		return yield(nil, fmt.Errorf("recursion depth limit exceeded"))
	}
	key := v.pop()
	target := v.pop()
	if target == nil {
		return yield(nil, fmt.Errorf("indexing nil"))
	}
	if p, ok := target.(*Pointer); ok {
		if p.ArrayType != nil {
			if list, ok := p.Target.(*List); ok {
				base, _ := ToInt64(p.Key)
				offset, ok := ToInt64(key)
				if !ok {
					return yield(nil, fmt.Errorf("index must be integer"))
				}
				idx := int(base + offset)
				if idx < 0 || idx >= len(list.Elements) {
					return yield(nil, fmt.Errorf("index out of bounds"))
				}
				v.push(list.Elements[idx])
				return true
			}
		}
		v.push(p)
		if !v.opDeref(yield, depth+1) {
			return false
		}
		derefed := v.pop()
		v.push(derefed)
		v.push(key)
		return v.opGetIndex(yield, depth+1)
	}
	type indexer interface {
		GetIndex(any) (any, bool)
	}
	if it, ok := target.(indexer); ok {
		val, ok := it.GetIndex(key)
		if !ok {
			return yield(nil, fmt.Errorf("index out of bounds"))
		}
		v.push(val)
		return true
	}
	return v.opGetIndexFallbacks(target, key, yield)
}

func (v *VM) opGetIndexFallbacks(target, key any, yield func(*Interrupt, error) bool) bool {
	switch t := target.(type) {
	case *Struct:
		if k, ok := key.(string); ok {
			v.push(t.Fields[k])
		} else {
			return yield(nil, fmt.Errorf("struct index must be string"))
		}
	case *List:
		idx, ok := ToInt64(key)
		if !ok {
			return yield(nil, fmt.Errorf("list index must be integer"))
		}
		length := int64(len(t.Elements))
		if idx < 0 {
			idx += length
		}
		if idx < 0 || idx >= length {
			return yield(nil, fmt.Errorf("index out of bounds"))
		}
		v.push(t.Elements[idx])
	case []any:
		idx, ok := ToInt64(key)
		if !ok {
			return yield(nil, fmt.Errorf("slice index must be integer"))
		}
		length := int64(len(t))
		if idx < 0 {
			idx += length
		}
		if idx < 0 || idx >= length {
			return yield(nil, fmt.Errorf("index out of bounds"))
		}
		v.push(t[idx])
	case map[any]any:
		v.push(t[key])
	case map[string]any:
		if k, ok := key.(string); ok {
			v.push(t[k])
		} else {
			v.push(nil)
		}
	case *Range:
		idx, ok := ToInt64(key)
		if !ok {
			return yield(nil, fmt.Errorf("range index must be integer"))
		}
		length := t.Len()
		if idx < 0 {
			idx += length
		}
		if idx < 0 || idx >= length {
			return yield(nil, fmt.Errorf("index out of bounds"))
		}
		v.push(t.Start + idx*t.Step)
	default:
		rv := reflect.ValueOf(target)
		if rv.Kind() == reflect.Array || rv.Kind() == reflect.Slice {
			idx, ok := ToInt64(key)
			if !ok {
				return yield(nil, fmt.Errorf("index must be integer"))
			}
			length := int64(rv.Len())
			if idx < 0 {
				idx += length
			}
			if idx < 0 || idx >= length {
				return yield(nil, fmt.Errorf("index out of bounds"))
			}
			v.push(rv.Index(int(idx)).Interface())
			return true
		}
		if rv.Kind() == reflect.Map {
			rk := reflect.ValueOf(key)
			if !rk.IsValid() {
				rk = reflect.Zero(rv.Type().Key())
			}
			if rk.Type().AssignableTo(rv.Type().Key()) {
				mv := rv.MapIndex(rk)
				if mv.IsValid() {
					v.push(mv.Interface())
				} else {
					v.push(reflect.Zero(rv.Type().Elem()).Interface())
				}
				return true
			}
		}
		return yield(nil, fmt.Errorf("type %T is not indexable", target))
	}
	return true
}

func (v *VM) opGetIndexOk(yield func(*Interrupt, error) bool, depth int) bool {
	if depth > 100 {
		return yield(nil, fmt.Errorf("recursion depth limit exceeded"))
	}
	key := v.pop()
	target := v.pop()
	if target == nil {
		v.push(nil)
		v.push(false)
		return true
	}
	if p, ok := target.(*Pointer); ok {
		v.push(p)
		success := true
		if !v.opDeref(func(i *Interrupt, e error) bool {
			success = false
			return false
		}, depth+1) && !success {
			v.push(nil)
			v.push(false)
			return true
		}
		derefed := v.pop()
		v.push(derefed)
		v.push(key)
		return v.opGetIndexOk(yield, depth+1)
	}
	switch m := target.(type) {
	case *Struct:
		if s, ok := key.(string); ok {
			val, ok := m.Fields[s]
			v.push(val)
			v.push(ok)
		} else {
			v.push(nil)
			v.push(false)
		}
	case map[any]any:
		val, ok := m[key]
		v.push(val)
		v.push(ok)
	case map[string]any:
		if s, ok := key.(string); ok {
			val, ok := m[s]
			v.push(val)
			v.push(ok)
		} else {
			v.push(nil)
			v.push(false)
		}
	default:
		rv := reflect.ValueOf(target)
		if rv.Kind() == reflect.Map {
			rk := reflect.ValueOf(key)
			if !rk.IsValid() {
				rk = reflect.Zero(rv.Type().Key())
			}
			if rk.Type().AssignableTo(rv.Type().Key()) {
				mv := rv.MapIndex(rk)
				if mv.IsValid() {
					v.push(mv.Interface())
					v.push(true)
				} else {
					v.push(reflect.Zero(rv.Type().Elem()).Interface())
					v.push(false)
				}
				return true
			}
		}

		v.push(nil)
		v.push(false)
	}
	return true
}

func (v *VM) opSetIndex(yield func(*Interrupt, error) bool, depth int) bool {
	if depth > 100 {
		return yield(nil, fmt.Errorf("recursion depth limit exceeded"))
	}
	val := v.pop()
	key := v.pop()
	target := v.pop()
	if target == nil {
		return yield(nil, fmt.Errorf("assignment to nil"))
	}
	if p, ok := target.(*Pointer); ok {
		if p.ArrayType != nil {
			if list, ok := p.Target.(*List); ok {
				base, _ := ToInt64(p.Key)
				offset, ok := ToInt64(key)
				if !ok {
					return yield(nil, fmt.Errorf("index must be integer"))
				}
				idx := int(base + offset)
				if idx < 0 || idx >= len(list.Elements) {
					return yield(nil, fmt.Errorf("index out of bounds"))
				}
				list.Elements[idx] = val
				return true
			}
		}
		v.push(p)
		if !v.opDeref(yield, depth+1) {
			return false
		}
		derefed := v.pop()
		v.push(derefed)
		v.push(key)
		v.push(val)
		return v.opSetIndex(yield, depth+1)
	}
	return v.opSetIndexFallbacks(target, key, val, yield)
}

func (v *VM) opSetIndexFallbacks(target, key, val any, yield func(*Interrupt, error) bool) bool {
	switch t := target.(type) {
	case *Struct:
		if k, ok := key.(string); ok {
			if t.Fields == nil {
				t.Fields = make(map[string]any)
			}
			t.Fields[k] = val
		} else {
			return yield(nil, fmt.Errorf("struct index must be string"))
		}
	case *List:
		if t.Immutable {
			return yield(nil, fmt.Errorf("tuple is immutable"))
		}
		idx, ok := ToInt64(key)
		if !ok {
			return yield(nil, fmt.Errorf("list index must be integer"))
		}
		length := int64(len(t.Elements))
		if idx < 0 {
			idx += length
		}
		if idx < 0 || idx >= length {
			return yield(nil, fmt.Errorf("index out of bounds"))
		}
		t.Elements[idx] = val
	case []any:
		idx, ok := ToInt64(key)
		if !ok {
			return yield(nil, fmt.Errorf("slice index must be integer"))
		}
		length := int64(len(t))
		if idx < 0 {
			idx += length
		}
		if idx < 0 || idx >= length {
			return yield(nil, fmt.Errorf("index out of bounds"))
		}
		t[idx] = val
	case map[any]any:
		t[key] = val
	case map[string]any:
		if k, ok := key.(string); ok {
			t[k] = val
		} else {
			return yield(nil, fmt.Errorf("key must be string"))
		}
	default:
		rv := reflect.ValueOf(target)
		if rv.Kind() == reflect.Array || rv.Kind() == reflect.Slice {
			idx, ok := ToInt64(key)
			if !ok {
				return yield(nil, fmt.Errorf("index must be integer"))
			}
			length := int64(rv.Len())
			if idx < 0 {
				idx += length
			}
			if idx < 0 || idx >= length {
				return yield(nil, fmt.Errorf("index out of bounds"))
			}
			rvv := reflect.ValueOf(val)
			if !rvv.IsValid() {
				rvv = reflect.Zero(rv.Type().Elem())
			}
			rv.Index(int(idx)).Set(rvv)
			return true
		}
		if rv.Kind() == reflect.Map {
			rk := reflect.ValueOf(key)
			if !rk.IsValid() {
				rk = reflect.Zero(rv.Type().Key())
			}
			rvv := reflect.ValueOf(val)
			if !rvv.IsValid() {
				rvv = reflect.Zero(rv.Type().Elem())
			}
			if !rk.Type().AssignableTo(rv.Type().Key()) {
				return yield(nil, fmt.Errorf("key must be %v", rv.Type().Key()))
			}
			if !rvv.Type().AssignableTo(rv.Type().Elem()) {
				return yield(nil, fmt.Errorf("value must be %v", rv.Type().Elem()))
			}
			rv.SetMapIndex(rk, rvv)
			return true
		}
		return yield(nil, fmt.Errorf("type %T does not support assignment", target))
	}
	return true
}

func (v *VM) opSwap(yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		if !yield(nil, fmt.Errorf("stack underflow during swap")) {
			return false
		}
		return true
	}
	top := v.SP - 1
	under := v.SP - 2
	v.OperandStack[top], v.OperandStack[under] = v.OperandStack[under], v.OperandStack[top]
	return true
}

func (v *VM) opDumpTrace(yield func(*Interrupt, error) bool) bool {
	var msg string
	for _, frame := range v.CallStack {
		msg += fmt.Sprintf("%s:%d\n", frame.Fun.Name, frame.ReturnIP)
	}
	msg += fmt.Sprintf("%s:%d", v.CurrentFun.Name, v.IP-1)
	return yield(nil, fmt.Errorf("%s", msg))
}

func (v *VM) opBitwise(op OpCode, yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		return yield(nil, fmt.Errorf("stack underflow during bitwise op"))
	}
	b := v.pop()
	a := v.pop()
	if op == OpBitOr {
		if m1, ok1 := a.(map[any]any); ok1 {
			if m2, ok2 := b.(map[any]any); ok2 {
				newMap := make(map[any]any, len(m1)+len(m2))
				maps.Copy(newMap, m1)
				maps.Copy(newMap, m2)
				v.push(newMap)
				return true
			}
		}
	}
	if res, ok, err := bitwiseSameType(op, a, b); ok {
		if err != nil {
			yield(nil, err)
			v.push(nil)
			return true
		}
		v.push(res)
		return true
	}

	// Mixed integer bitwise
	if i1, ok1 := ToInt64(a); ok1 && a != nil {
		if i2, ok2 := ToInt64(b); ok2 && b != nil {
			var res any
			switch op {
			case OpBitAnd:
				res = i1 & i2
			case OpBitOr:
				res = i1 | i2
			case OpBitXor:
				res = i1 ^ i2
			case OpBitLsh:
				if i2 < 0 {
					yield(nil, fmt.Errorf("negative shift count: %d", i2))
					v.push(nil)
					return true
				}
				res = i1 << uint(i2)
			case OpBitRsh:
				if i2 < 0 {
					yield(nil, fmt.Errorf("negative shift count: %d", i2))
					v.push(nil)
					return true
				}
				res = i1 >> uint(i2)
			}
			v.push(res)
			return true
		}
	}

	i1, ok1 := ToIntBig(a)
	i2, ok2 := ToIntBig(b)
	if !ok1 || !ok2 {
		yield(nil, fmt.Errorf("bitwise operands must be integers"))
		v.push(nil)
		return true
	}
	var res *big.Int
	switch op {
	case OpBitAnd:
		res = new(big.Int).And(i1, i2)
	case OpBitOr:
		res = new(big.Int).Or(i1, i2)
	case OpBitXor:
		res = new(big.Int).Xor(i1, i2)
	case OpBitLsh:
		res = new(big.Int).Lsh(i1, uint(i2.Uint64()))
	case OpBitRsh:
		res = new(big.Int).Rsh(i1, uint(i2.Uint64()))
	}
	if res != nil {
		if res.IsInt64() {
			v.push(res.Int64())
		} else {
			v.push(res)
		}
	}
	return true
}

func (v *VM) opBitNot(yield func(*Interrupt, error) bool) bool {
	if v.SP < 1 {
		if !yield(nil, fmt.Errorf("stack underflow during bitwise not")) {
			return false
		}
		return true
	}
	a := v.pop()

	var res any
	switch i := a.(type) {
	case int:
		res = ^i
	case int8:
		res = ^i
	case int16:
		res = ^i
	case int32:
		res = ^i
	case int64:
		res = ^i
	case uint:
		res = ^i
	case uint8:
		res = ^i
	case uint16:
		res = ^i
	case uint32:
		res = ^i
	case uint64:
		res = ^i
	case *big.Int:
		res = new(big.Int).Not(i)
	default:
		if !yield(nil, fmt.Errorf("bitwise not operand must be int, got %T", a)) {
			return false
		}
		v.push(nil)
		return true
	}
	if r, ok := res.(*big.Int); ok {
		if r.IsInt64() {
			v.push(r.Int64())
		} else {
			v.push(r)
		}
	} else {
		v.push(res)
	}
	return true
}

func (v *VM) opMath(op OpCode, yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		return yield(nil, fmt.Errorf("stack underflow during math op"))
	}
	b := v.pop()
	a := v.pop()

	if res, ok, err := arithmeticSameType(op, a, b); ok {
		if err != nil {
			yield(nil, err)
			v.push(nil)
			return true
		}
		v.push(res)
		return true
	}

	if op == OpAdd {
		if l1, ok := a.(*List); ok {
			if l2, ok := b.(*List); ok {
				res := make([]any, 0, len(l1.Elements)+len(l2.Elements))
				res = append(res, l1.Elements...)
				res = append(res, l2.Elements...)
				v.push(&List{Elements: res})
				return true
			}
		}
	}

	if isComplex(a) || isComplex(b) {
		c1, ok1 := toComplex128(a)
		c2, ok2 := toComplex128(b)
		if !ok1 || !ok2 {
			yield(nil, fmt.Errorf("invalid operands for complex math"))
			v.push(nil)
			return true
		}
		var res complex128
		switch op {
		case OpAdd:
			res = c1 + c2
		case OpSub:
			res = c1 - c2
		case OpMul:
			res = c1 * c2
		case OpDiv:
			if c2 == 0 {
				yield(nil, fmt.Errorf("division by zero"))
				v.push(nil)
				return true
			}
			res = c1 / c2
		default:
			yield(nil, fmt.Errorf("unsupported operation for complex numbers"))
			v.push(nil)
			return true
		}
		v.push(res)
		return true
	}

	// Mixed integer math to avoid float conversion for (int, int64) etc.
	if i1, ok1 := ToInt64(a); ok1 && a != nil {
		if i2, ok2 := ToInt64(b); ok2 && b != nil {
			// Ensure neither is a float type to avoid losing precision/type
			_, isFloatA := a.(float64)
			if !isFloatA {
				_, isFloatA = a.(float32)
			}
			_, isFloatB := b.(float64)
			if !isFloatB {
				_, isFloatB = b.(float32)
			}
			if !isFloatA && !isFloatB && op != OpPow {
				switch op {
				case OpAdd:
					v.push(i1 + i2)
					return true
				case OpSub:
					v.push(i1 - i2)
					return true
				case OpMul:
					v.push(i1 * i2)
					return true
				case OpDiv:
					if i2 == 0 {
						yield(nil, fmt.Errorf("division by zero"))
						v.push(nil)
					} else {
						v.push(i1 / i2)
					}
					return true
				case OpMod:
					if i2 == 0 {
						yield(nil, fmt.Errorf("division by zero"))
						v.push(nil)
					} else {
						r := i1 % i2
						if (r < 0) != (i2 < 0) && r != 0 {
							r += i2
						}
						v.push(r)
					}
					return true
				case OpFloorDiv:
					if i2 == 0 {
						yield(nil, fmt.Errorf("division by zero"))
						v.push(nil)
					} else {
						q := i1 / i2
						if (i1 < 0) != (i2 < 0) && i1%i2 != 0 {
							q--
						}
						v.push(q)
					}
					return true
				}
			}
		}
	}

	// Try non-allocating numeric math for mixed scalar types
	if fa, ok1 := ToFloat64(a); ok1 {
		if fb, ok2 := ToFloat64(b); ok2 {
			_, isBigA := a.(*big.Float)
			_, isBigB := b.(*big.Float)
			_, isBigIntA := a.(*big.Int)
			_, isBigIntB := b.(*big.Int)
			if !isBigA && !isBigB && !isBigIntA && !isBigIntB {
				switch op {
				case OpAdd:
					v.push(fa + fb)
					return true
				case OpSub:
					v.push(fa - fb)
					return true
				case OpMul:
					v.push(fa * fb)
					return true
				case OpDiv:
					if fb == 0 {
						yield(nil, fmt.Errorf("division by zero"))
						v.push(nil)
					} else {
						v.push(fa / fb)
					}
					return true
				case OpPow:
					// If both are integers, fall through to big.Int handling for precision
					_, okA := ToInt64(a)
					_, okB := ToInt64(b)
					if !okA || !okB {
						v.push(math.Pow(fa, fb))
						return true
					}
				}
			}
		}
	}

	i1, ok1 := ToIntBig(a)
	i2, ok2 := ToIntBig(b)
	if ok1 && ok2 {
		var res *big.Int
		switch op {
		case OpAdd:
			res = new(big.Int).Add(i1, i2)
		case OpSub:
			res = new(big.Int).Sub(i1, i2)
		case OpMul:
			res = new(big.Int).Mul(i1, i2)
		case OpDiv:
			if i2.Sign() == 0 {
				yield(nil, fmt.Errorf("division by zero"))
				v.push(nil)
			} else {
				res = new(big.Int).Quo(i1, i2)
			}
		case OpMod:
			if i2.Sign() == 0 {
				yield(nil, fmt.Errorf("division by zero"))
				v.push(nil)
			} else {
				res = new(big.Int).Rem(i1, i2)
			}
		case OpPow:
			exp := i2.Int64()
			if i2.IsInt64() && exp >= 0 {
				res = new(big.Int).Exp(i1, big.NewInt(exp), nil)
			} else {
				f1, _ := new(big.Float).SetInt(i1).Float64()
				f2, _ := new(big.Float).SetInt(i2).Float64()
				v.push(math.Pow(f1, f2))
				return true
			}
		}
		if res != nil {
			if res.IsInt64() {
				v.push(res.Int64())
			} else {
				v.push(res)
			}
		}
		return true
	}

	if fa, ok1 := ToFloatBig(a); ok1 {
		if fb, ok2 := ToFloatBig(b); ok2 {
			var res *big.Float
			switch op {
			case OpAdd:
				res = new(big.Float).Add(fa, fb)
			case OpSub:
				res = new(big.Float).Sub(fa, fb)
			case OpMul:
				res = new(big.Float).Mul(fa, fb)
			case OpDiv:
				if fb.Sign() == 0 {
					yield(nil, fmt.Errorf("division by zero"))
					v.push(nil)
					return true
				}
				res = new(big.Float).Quo(fa, fb)
			case OpPow:
				f1, _ := fa.Float64()
				f2, _ := fb.Float64()
				v.push(math.Pow(f1, f2))
				return true
			default:
				yield(nil, fmt.Errorf("unsupported operation for floats"))
				v.push(nil)
				return true
			}
			if res != nil {
				_, isBigA := a.(*big.Float)
				_, isBigB := b.(*big.Float)
				_, isBigIntA := a.(*big.Int)
				_, isBigIntB := b.(*big.Int)
				if !isBigA && !isBigB && !isBigIntA && !isBigIntB {
					f, _ := res.Float64()
					v.push(f)
				} else {
					v.push(res)
				}
			}
			return true
		}
	}

	yield(nil, fmt.Errorf("math operands must be numeric"))
	v.push(nil)
	return true
}

func (v *VM) opCompare(op OpCode, yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		return yield(nil, fmt.Errorf("stack underflow during comparison"))
	}
	b := v.pop()
	a := v.pop()
	if op == OpEq {
		v.push(a == b || v.compareEqFallback(a, b))
		return true
	}
	if op == OpNe {
		v.push(a != b && !v.compareEqFallback(a, b))
		return true
	}
	if s1, ok := a.(string); ok {
		if s2, ok := b.(string); ok {
			switch op {
			case OpLt:
				v.push(s1 < s2)
			case OpLe:
				v.push(s1 <= s2)
			case OpGt:
				v.push(s1 > s2)
			case OpGe:
				v.push(s1 >= s2)
			}
			return true
		}
	}
	if isComplex(a) || isComplex(b) {
		yield(nil, fmt.Errorf("complex numbers are not ordered"))
		v.push(nil)
		return true
	}

	// Mixed integer comparison
	if i1, ok1 := ToInt64(a); ok1 && a != nil {
		if i2, ok2 := ToInt64(b); ok2 && b != nil {
			_, isFloatA := a.(float64)
			if !isFloatA {
				_, isFloatA = a.(float32)
			}
			_, isFloatB := b.(float64)
			if !isFloatB {
				_, isFloatB = b.(float32)
			}
			if !isFloatA && !isFloatB {
				var res bool
				switch op {
				case OpLt:
					res = i1 < i2
				case OpLe:
					res = i1 <= i2
				case OpGt:
					res = i1 > i2
				case OpGe:
					res = i1 >= i2
				default:
					return false
				}
				v.push(res)
				return true
			}
		}
	}

	// Try non-allocating float64 comparison for mixed numeric types
	if !isComplex(a) && !isComplex(b) {
		if fa, ok1 := ToFloat64(a); ok1 {
			if fb, ok2 := ToFloat64(b); ok2 {
				// Only use float64 if neither is a big type to preserve precision
				_, isBigA := a.(*big.Float)
				_, isBigB := b.(*big.Float)
				_, isBigIntA := a.(*big.Int)
				_, isBigIntB := b.(*big.Int)
				if !isBigA && !isBigB && !isBigIntA && !isBigIntB {
					switch op {
					case OpLt:
						v.push(fa < fb)
					case OpLe:
						v.push(fa <= fb)
					case OpGt:
						v.push(fa > fb)
					case OpGe:
						v.push(fa >= fb)
					}
					return true
				}
			}
		}
	}

	if fa, ok1 := ToFloatBig(a); ok1 {
		if fb, ok2 := ToFloatBig(b); ok2 {
			cmp := fa.Cmp(fb)
			switch op {
			case OpLt:
				v.push(cmp < 0)
			case OpLe:
				v.push(cmp <= 0)
			case OpGt:
				v.push(cmp > 0)
			case OpGe:
				v.push(cmp >= 0)
			}
			return true
		}
	}
	i1, ok1 := ToIntBig(a)
	i2, ok2 := ToIntBig(b)
	if ok1 && ok2 {
		cmp := i1.Cmp(i2)
		switch op {
		case OpLt:
			v.push(cmp < 0)
		case OpLe:
			v.push(cmp <= 0)
		case OpGt:
			v.push(cmp > 0)
		case OpGe:
			v.push(cmp >= 0)
		}
		return true
	}
	yield(nil, fmt.Errorf("unsupported type for comparison"))
	v.push(nil)
	return true
}

func (v *VM) compareEqFallback(a, b any) bool {
	i1, ok1 := ToIntBig(a)
	i2, ok2 := ToIntBig(b)
	if ok1 && ok2 {
		return i1.Cmp(i2) == 0
	}
	f1, ok1 := ToFloatBig(a)
	f2, ok2 := ToFloatBig(b)
	if ok1 && ok2 {
		return f1.Cmp(f2) == 0
	}
	return false
}

func (v *VM) opNot(yield func(*Interrupt, error) bool) bool {
	if v.SP < 1 {
		if !yield(nil, fmt.Errorf("stack underflow during not")) {
			return false
		}
		return true
	}
	val := v.pop()
	v.push(isZero(val))
	return true
}

func (v *VM) opGetIter(yield func(*Interrupt, error) bool) bool {
	if v.SP < 1 {
		if !yield(nil, fmt.Errorf("stack underflow during getiter")) {
			return false
		}
		return true
	}
	val := v.pop()
	if val == nil {
		if !yield(nil, fmt.Errorf("not iterable: nil")) {
			return false
		}
		v.push(nil)
		return true
	}

	switch t := val.(type) {
	case *List:
		v.push(&ListIterator{List: t})
	case []any:
		v.push(&ListIterator{List: &List{Elements: t, Immutable: false}})
	case map[any]any:
		keys := v.getMapKeys(t)
		v.push(&MapIterator{Map: t, Keys: keys})
	case map[string]any:
		keys := v.getMapStringKeys(t)
		v.push(&MapIterator{Map: t, Keys: keys})
	case *Range:
		v.push(&RangeIterator{
			Range: t,
			Curr:  t.Start,
		})
	case *Closure, NativeFunc:
		v.push(v.newFuncIterator(t))
	default:
		if i, ok := ToInt64(val); ok {
			v.push(&RangeIterator{
				Range: &Range{Start: 0, Stop: i, Step: 1},
				Curr:  0,
			})
			return true
		}
		if !yield(nil, fmt.Errorf("type %T is not iterable", val)) {
			return false
		}
		v.push(nil)
	}
	return true
}

func (v *VM) opNextIter(inst OpCode, yield func(*Interrupt, error) bool) bool {
	offset := int(int32(inst) >> 8)
	if v.SP < 1 {
		if !yield(nil, fmt.Errorf("stack underflow during nextiter")) {
			return false
		}
		return true
	}
	iter := v.OperandStack[v.SP-1]

	switch it := iter.(type) {
	case *ListIterator:
		if it.Idx < len(it.List.Elements) {
			v.push(it.List.Elements[it.Idx])
			it.Idx++
		} else {
			v.pop()
			v.IP += offset
		}
	case *MapIterator:
		if it.Idx < len(it.Keys) {
			v.push(it.Keys[it.Idx])
			it.Idx++
		} else {
			v.pop()
			v.IP += offset
		}
	case *RangeIterator:
		if (it.Range.Step > 0 && it.Curr < it.Range.Stop) || (it.Range.Step < 0 && it.Curr > it.Range.Stop) {
			v.push(it.Curr)
			it.Curr += it.Range.Step
		} else {
			v.pop()
			v.IP += offset
		}
	case *FuncIterator:
		it.EnsureYieldBound()
		it.Resumed = true
		var innerIntr *Interrupt
		var innerErr error
		it.InnerVM.Run(func(intr *Interrupt, err error) bool {
			if err == ErrYield {
				it.Resumed = false
				return false
			}
			if intr != nil || err != nil {
				innerIntr, innerErr = intr, err
				return false
			}
			return true
		})
		if innerIntr != nil || innerErr != nil {
			if !yield(innerIntr, innerErr) {
				v.IP--
				return false
			}
			return true
		}
		if !it.Resumed {
			v.push(it.K)
			return true
		}
		it.Done = true
		v.pop()
		v.IP += offset
	default:
		if !yield(nil, fmt.Errorf("not an iterator: %T", iter)) {
			return false
		}
	}
	return true
}

func (v *VM) opMakeTuple(inst OpCode, yield func(*Interrupt, error) bool) bool {
	n := int(inst >> 8)
	if v.SP < n {
		if !yield(nil, fmt.Errorf("stack underflow during tuple creation")) {
			return false
		}
		return true
	}
	slice := make([]any, n)
	start := v.SP - n
	copy(slice, v.OperandStack[start:v.SP])
	v.drop(n)
	v.push(&List{Elements: slice, Immutable: true})
	return true
}

func (v *VM) opGetSlice(yield func(*Interrupt, error) bool) bool {
	if v.SP < 4 {
		if !yield(nil, fmt.Errorf("stack underflow during getslice")) {
			return false
		}
		return true
	}
	step := v.pop()
	hi := v.pop()
	lo := v.pop()
	target := v.pop()

	if target == nil {
		if !yield(nil, fmt.Errorf("slicing nil")) {
			return false
		}
		v.push(nil)
		return true
	}

	switch t := target.(type) {
	case string:
		runes := []rune(t)
		start, stop, stepInt, err := resolveSliceIndices(len(runes), lo, hi, step)
		if err != nil {
			if !yield(nil, err) {
				return false
			}
			v.push(nil)
			return true
		}
		var res []rune
		if stepInt > 0 {
			for i := start; i < stop; i += stepInt {
				res = append(res, runes[i])
			}
		} else {
			for i := start; i > stop; i += stepInt {
				res = append(res, runes[i])
			}
		}
		v.push(string(res))

	case *List:
		start, stop, stepInt, err := resolveSliceIndices(len(t.Elements), lo, hi, step)
		if err != nil {
			if !yield(nil, err) {
				return false
			}
			v.push(nil)
			return true
		}
		var res []any
		if stepInt > 0 {
			for i := start; i < stop; i += stepInt {
				res = append(res, t.Elements[i])
			}
		} else {
			for i := start; i > stop; i += stepInt {
				res = append(res, t.Elements[i])
			}
		}
		v.push(&List{Elements: res, Immutable: t.Immutable})

	case []any:
		start, stop, stepInt, err := resolveSliceIndices(len(t), lo, hi, step)
		if err != nil {
			if !yield(nil, err) {
				return false
			}
			v.push(nil)
			return true
		}
		var res []any
		if stepInt > 0 {
			for i := start; i < stop; i += stepInt {
				res = append(res, t[i])
			}
		} else {
			for i := start; i > stop; i += stepInt {
				res = append(res, t[i])
			}
		}
		v.push(res)

	default:
		if !yield(nil, fmt.Errorf("type %T is not sliceable", target)) {
			return false
		}
		v.push(nil)
	}
	return true
}

func (v *VM) opSetSlice(yield func(*Interrupt, error) bool) bool {
	if v.SP < 5 {
		if !yield(nil, fmt.Errorf("stack underflow during setslice")) {
			return false
		}
		return true
	}
	val := v.pop()
	step := v.pop()
	hi := v.pop()
	lo := v.pop()
	target := v.pop()

	var t []any
	var targetList *List

	switch lst := target.(type) {
	case *List:
		if lst.Immutable {
			if !yield(nil, fmt.Errorf("tuple is immutable")) {
				return false
			}
			return true
		}
		t = lst.Elements
		targetList = lst
	case []any:
		t = lst
	default:
		if !yield(nil, fmt.Errorf("type %T does not support slice assignment", target)) {
			return false
		}
		return true
	}

	start, stop, stepInt, err := resolveSliceIndices(len(t), lo, hi, step)
	if err != nil {
		if !yield(nil, err) {
			return false
		}
		return true
	}

	// Convert value to list of items
	var items []any
	switch v := val.(type) {
	case *List:
		items = v.Elements
	case []any:
		items = v
	case string:
		for _, r := range v {
			items = append(items, string(r))
		}
	default:
		if !yield(nil, fmt.Errorf("can only assign iterable to slice, got %T", val)) {
			return false
		}
		return true
	}

	if stepInt != 1 {
		// Extended slice assignment
		// Target count must match value count
		n := 0
		if stepInt > 0 {
			if stop > start {
				n = (stop - start + stepInt - 1) / stepInt
			}
		} else {
			if start > stop {
				n = (start - stop - stepInt - 1) / -stepInt
			}
		}
		if len(items) != n {
			if !yield(nil, fmt.Errorf("attempt to assign sequence of size %d to extended slice of size %d", len(items), n)) {
				return false
			}
			return true
		}
		for i := 0; i < n; i++ {
			t[start+i*stepInt] = items[i]
		}

	} else {
		// Standard slice assignment: replace [start:stop] with items
		if stop < start {
			stop = start
		}
		delta := len(items) - (stop - start)
		if delta != 0 {
			if targetList != nil {
				newLen := len(t) + delta
				newElems := make([]any, 0, newLen)
				newElems = append(newElems, t[:start]...)
				newElems = append(newElems, items...)
				newElems = append(newElems, t[stop:]...)
				targetList.Elements = newElems
				return true
			}
			if !yield(nil, fmt.Errorf("cannot resize raw slice, use List instead")) {
				return false
			}
			return true
		}
		for i := range items {
			t[start+i] = items[i]
		}
	}
	return true
}

func (v *VM) opGetAttr(yield func(*Interrupt, error) bool, depth int) bool {
	if depth > 100 {
		return yield(nil, fmt.Errorf("recursion depth limit exceeded"))
	}
	if v.SP < 2 {
		return yield(nil, fmt.Errorf("stack underflow during getattr"))
	}
	name := v.pop()
	target := v.pop()
	nameStr, ok := name.(string)
	if !ok {
		v.push(nil)
		return yield(nil, fmt.Errorf("attribute name must be string, got %T", name))
	}
	if target == nil {
		v.push(nil)
		return yield(nil, fmt.Errorf("getattr on nil"))
	}
	switch t := target.(type) {
	case *Struct:
		found, err := v.getStructAttr(t, nameStr, depth)
		if err != nil {
			return yield(nil, err)
		}
		if found {
			return true
		}
		return yield(nil, fmt.Errorf("struct %s has no field or method '%s'", t.TypeName, nameStr))
	case map[any]any:
		v.push(t[nameStr])
		return true
	case map[string]any:
		v.push(t[nameStr])
		return true
	case *List:
		return v.opGetListAttr(t, nameStr, yield)
	case *Pointer:
		v.push(target)
		if !v.opDeref(yield, depth+1) {
			return false
		}
		derefed := v.pop()
		v.push(derefed)
		v.push(name)
		return v.opGetAttr(yield, depth+1)
	case *Type:
		return v.opGetTypeAttr(t, nameStr, yield)
	case reflect.Type:
		return v.opGetTypeAttr(FromReflectType(t), nameStr, yield)
	default:
		return v.opGetNativeAttr(target, nameStr, yield)
	}
}

func (v *VM) getStructAttr(s *Struct, nameStr string, depth int) (bool, error) {
	if depth > 100 {
		return false, fmt.Errorf("recursion depth limit exceeded")
	}
	if val, ok := s.Fields[nameStr]; ok {
		v.push(val)
		return true, nil
	}
	if s.TypeName != "" {
		if v.tryGetStructMethod(s, nameStr) {
			return true, nil
		}
	}
	for _, emb := range s.Embedded {
		if fieldVal, ok := s.Fields[emb]; ok && fieldVal != nil {
			if embStruct, ok := fieldVal.(*Struct); ok {
				found, err := v.getStructAttr(embStruct, nameStr, depth+1)
				if err != nil {
					return false, err
				}
				if found {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (v *VM) opGetTypeAttr(t *Type, nameStr string, yield func(*Interrupt, error) bool) bool {
	typeName := t.Name
	if typeName == "" && t.Kind == KindPtr && t.Elem != nil {
		typeName = t.Elem.Name
	}
	if typeName != "" {
		if method, ok := v.Get(typeName + "." + nameStr); ok {
			v.push(method)
			return true
		}
		if method, ok := v.Get("*" + typeName + "." + nameStr); ok {
			v.push(method)
			return true
		}
	}
	if rt := t.ToReflectType(); rt != nil {
		if m, ok := rt.MethodByName(nameStr); ok {
			v.push(v.newNativeMethodExpr(m))
			return true
		}
	}
	return yield(nil, fmt.Errorf("type %v has no attribute '%s'", t, nameStr))
}

func (v *VM) tryGetStructMethod(s *Struct, nameStr string) bool {
	if val, ok := v.Get(s.TypeName + "." + nameStr); ok {
		v.push(&BoundMethod{
			Receiver: s.Copy(),
			Fun:      val,
		})
		return true
	}
	if val, ok := v.Get("*" + s.TypeName + "." + nameStr); ok {
		v.push(&BoundMethod{
			Receiver: s,
			Fun:      val,
		})
		return true
	}
	return false
}

func (v *VM) opGetListAttr(l *List, nameStr string, yield func(*Interrupt, error) bool) bool {
	if nameStr == "append" {
		v.push(&BoundMethod{
			Receiver: l,
			Fun: NativeFunc{
				Name: "list.append",
				Func: ListAppend,
			},
		})
		return true
	}
	return yield(nil, fmt.Errorf("list has no attribute '%s'", nameStr))
}

func (v *VM) newNativeMethodExpr(m reflect.Method) NativeFunc {
	return NativeFunc{
		Name: m.Name,
		Func: func(vm *VM, args []any) (any, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("method expression requires receiver as first argument")
			}
			in := make([]reflect.Value, len(args))
			for i, arg := range args {
				var argType reflect.Type
				if i < m.Type.NumIn() {
					argType = m.Type.In(i)
				} else if m.Type.IsVariadic() {
					argType = m.Type.In(m.Type.NumIn() - 1).Elem()
				}
				if arg == nil {
					in[i] = reflect.Zero(argType)
				} else {
					in[i] = reflect.ValueOf(arg)
				}
			}
			return vm.handleNativeReturn(m.Func.Call(in))
		},
	}
}

func (v *VM) opGetNativeAttr(target any, nameStr string, yield func(*Interrupt, error) bool) bool {
	rv := reflect.ValueOf(target)
	m := rv.MethodByName(nameStr)
	if m.IsValid() {
		v.push(&BoundMethod{
			Receiver: target,
			Fun: NativeFunc{
				Name: nameStr,
				Func: func(vm *VM, args []any) (any, error) {
					// BoundMethod prepends receiver, skip it for already bound native method
					in := make([]reflect.Value, len(args)-1)
					mt := m.Type()
					for i := 1; i < len(args); i++ {
						var argType reflect.Type
						if i-1 < mt.NumIn() {
							argType = mt.In(i - 1)
						} else if mt.IsVariadic() {
							argType = mt.In(mt.NumIn() - 1).Elem()
						}
						if args[i] == nil {
							in[i-1] = reflect.Zero(argType)
						} else {
							in[i-1] = reflect.ValueOf(args[i])
						}
					}
					return vm.handleNativeReturn(m.Call(in))
				},
			},
		})
		return true
	}
	return v.opGetNativeField(rv, nameStr, target, yield)
}

func (v *VM) opGetNativeField(rv reflect.Value, nameStr string, target any, yield func(*Interrupt, error) bool) bool {
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct {
		if f := rv.FieldByName(nameStr); f.IsValid() {
			v.push(f.Interface())
			return true
		}
	}
	return yield(nil, fmt.Errorf("type %T has no attributes", target))
}

func (v *VM) opSetAttr(yield func(*Interrupt, error) bool, depth int) bool {
	if depth > 100 {
		return yield(nil, fmt.Errorf("recursion depth limit exceeded"))
	}
	if v.SP < 3 {
		if !yield(nil, fmt.Errorf("stack underflow during setattr")) {
			return false
		}
		return true
	}
	val := v.pop()
	name := v.pop()
	target := v.pop()

	nameStr, ok := name.(string)
	if !ok {
		if !yield(nil, fmt.Errorf("attribute name must be string, got %T", name)) {
			return false
		}
		return true
	}

	if target == nil {
		if !yield(nil, fmt.Errorf("setattr on nil")) {
			return false
		}
		return true
	}

	switch t := target.(type) {
	case *Struct:
		found, err := v.setStructAttr(t, nameStr, val, depth)
		if err != nil {
			return yield(nil, err)
		}
		if found {
			return true
		}
		if t.Fields == nil {
			t.Fields = make(map[string]any)
		}
		t.Fields[nameStr] = val

	case map[any]any:
		t[nameStr] = val
		return true

	case map[string]any:
		t[nameStr] = val
		return true

	case *Pointer:
		v.push(t)
		if !v.opDeref(yield, depth+1) {
			return false
		}
		derefed := v.pop()
		rv := reflect.ValueOf(derefed)
		if rv.Kind() == reflect.Struct {
			newObj := reflect.New(rv.Type()).Elem()
			newObj.Set(rv)
			if f := newObj.FieldByName(nameStr); f.IsValid() && f.CanSet() {
				fv := reflect.ValueOf(val)
				if !fv.IsValid() {
					f.Set(reflect.Zero(f.Type()))
				} else if fv.Type().AssignableTo(f.Type()) {
					f.Set(fv)
				} else if fv.Type().ConvertibleTo(f.Type()) {
					f.Set(fv.Convert(f.Type()))
				}
				v.push(t)
				v.push(newObj.Interface())
				return v.opSetDeref(yield, depth+1)
			}
		}
		v.push(derefed)
		v.push(name)
		v.push(val)
		return v.opSetAttr(yield, depth+1)

	default:
		rv := reflect.ValueOf(target)
		if rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
		}
		if rv.Kind() == reflect.Struct {
			if f := rv.FieldByName(nameStr); f.IsValid() && f.CanSet() {
				fv := reflect.ValueOf(val)
				if !fv.IsValid() {
					f.Set(reflect.Zero(f.Type()))
				} else if fv.Type().AssignableTo(f.Type()) {
					f.Set(fv)
				} else if fv.Type().ConvertibleTo(f.Type()) {
					f.Set(fv.Convert(f.Type()))
				} else {
					return yield(nil, fmt.Errorf("cannot assign %T to field %s of type %v", val, nameStr, f.Type()))
				}
				return true
			}
		}
		if !yield(nil, fmt.Errorf("type %T does not support attribute assignment", target)) {
			return false
		}
	}
	return true
}

func (v *VM) setStructAttr(s *Struct, nameStr string, val any, depth int) (bool, error) {
	if depth > 100 {
		return false, fmt.Errorf("recursion depth limit exceeded")
	}
	if _, ok := s.Fields[nameStr]; ok {
		s.Fields[nameStr] = val
		return true, nil
	}
	for _, emb := range s.Embedded {
		if fieldVal, ok := s.Fields[emb]; ok && fieldVal != nil {
			if embStruct, ok := fieldVal.(*Struct); ok {
				found, err := v.setStructAttr(embStruct, nameStr, val, depth+1)
				if err != nil {
					return false, err
				}
				if found {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (v *VM) opCallKw(inst OpCode, yield func(*Interrupt, error) bool) bool {
	if v.SP < 3 {
		if !yield(nil, fmt.Errorf("stack underflow during callkw")) {
			return false
		}
		return true
	}

	kwArgObj := v.pop()
	posArgObj := v.pop()
	callee := v.pop()

	kwArgs, ok := kwArgObj.(map[any]any)
	if !ok {
		if !yield(nil, fmt.Errorf("kw_args must be map")) {
			return false
		}
		v.push(nil)
		return true
	}

	var posArgs []any
	switch l := posArgObj.(type) {
	case *List:
		posArgs = l.Elements
	case []any:
		posArgs = l
	default:
		if !yield(nil, fmt.Errorf("pos_args must be list")) {
			return false
		}
		v.push(nil)
		return true
	}

	switch fn := callee.(type) {
	case *BoundMethod:
		newElems := make([]any, 0, len(posArgs)+1)
		newElems = append(newElems, fn.Receiver)
		newElems = append(newElems, posArgs...)

		v.push(fn.Fun)
		v.push(&List{Elements: newElems, Immutable: true})
		v.push(kwArgObj)
		return v.opCallKw(inst, yield)

	case *Closure:
		numParams := fn.Fun.NumParams
		paramNames := fn.Fun.ParamNames
		isVariadic := fn.Fun.Variadic

		numFixed := numParams
		if isVariadic {
			numFixed--
		}

		locals := make([]any, numParams)
		isSet := make([]bool, numParams)

		if len(posArgs) > numFixed {
			if !isVariadic {
				if !yield(nil, fmt.Errorf("too many arguments: want %d, got %d", numFixed, len(posArgs))) {
					return false
				}
				v.push(nil)
				return true
			}
			for i := 0; i < numFixed; i++ {
				locals[i] = posArgs[i]
				isSet[i] = true
			}
			varargSlice := make([]any, len(posArgs)-numFixed)
			copy(varargSlice, posArgs[numFixed:])
			locals[numFixed] = &List{Elements: varargSlice, Immutable: true}
			isSet[numFixed] = true
		} else {
			for i := 0; i < len(posArgs); i++ {
				locals[i] = posArgs[i]
				isSet[i] = true
			}
			if isVariadic {
				locals[numFixed] = &List{Elements: []any{}, Immutable: true}
				isSet[numFixed] = true
			}
		}

		for k, val := range kwArgs {
			name, ok := k.(string)
			if !ok {
				if !yield(nil, fmt.Errorf("keyword must be string")) {
					return false
				}
				v.push(nil)
				return true
			}
			idx := -1
			for i, pname := range paramNames {
				if pname == name {
					idx = i
					break
				}
			}
			if idx == -1 {
				if !yield(nil, fmt.Errorf("unexpected keyword argument '%s'", name)) {
					return false
				}
				v.push(nil)
				return true
			}
			if isVariadic && idx == numFixed {
				if !yield(nil, fmt.Errorf("unexpected keyword argument '%s' (variadic parameter)", name)) {
					return false
				}
				v.push(nil)
				return true
			}
			if isSet[idx] {
				if !yield(nil, fmt.Errorf("multiple values for argument '%s'", name)) {
					return false
				}
				v.push(nil)
				return true
			}
			locals[idx] = val
			isSet[idx] = true
		}

		startDefaults := numFixed - len(fn.Defaults)
		for i := 0; i < numFixed; i++ {
			if !isSet[i] {
				if i >= startDefaults {
					locals[i] = fn.Defaults[i-startDefaults]
					isSet[i] = true
				} else {
					if !yield(nil, fmt.Errorf("missing argument '%s'", paramNames[i])) {
						return false
					}
					v.push(nil)
					return true
				}
			}
		}

		newEnv := v.allocEnv(fn.Env)
		for i, name := range paramNames {
			if i < len(locals) {
				newEnv.Def(name, locals[i])
			}
		}

		v.push(fn)
		calleeIdx := v.SP - 1

		if v.SP+len(locals) > len(v.OperandStack) {
			needed := v.SP + len(locals)
			newCap := max(len(v.OperandStack)*2, needed)
			newStack := make([]any, newCap)
			copy(newStack, v.OperandStack)
			v.OperandStack = newStack
		}
		copy(v.OperandStack[v.SP:], locals)
		v.SP += len(locals)

		v.CallStack = append(v.CallStack, Frame{
			Fun:      v.CurrentFun,
			ReturnIP: v.IP,
			Env:      v.Scope,
			BaseSP:   calleeIdx,
			BP:       v.BP,
			Defers:   v.Defers,
		})

		v.CurrentFun = fn.Fun
		v.IP = 0
		v.Scope = newEnv
		v.BP = calleeIdx + 1
		v.Defers = nil

		return true

	case NativeFunc:
		if len(kwArgs) > 0 {
			if !yield(nil, fmt.Errorf("native functions do not support keyword arguments")) {
				return false
			}
			v.push(nil)
			return true
		}
		res, err := fn.Call(v, posArgs)
		if err != nil {
			if !yield(nil, err) {
				return false
			}
			v.push(nil)
			return true
		}
		v.push(res)
		return true

	default:
		if !yield(nil, fmt.Errorf("not a function: %T", callee)) {
			return false
		}
		v.push(nil)
		return true
	}
}

func (v *VM) opListAppend(yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		if !yield(nil, fmt.Errorf("stack underflow during list append")) {
			return false
		}
		return true
	}
	val := v.pop()
	listObj := v.OperandStack[v.SP-1]
	l, ok := listObj.(*List)
	if !ok {
		if !yield(nil, fmt.Errorf("append target must be list, got %T", listObj)) {
			return false
		}
		return true
	}
	if l.Immutable {
		if !yield(nil, fmt.Errorf("list is immutable")) {
			return false
		}
		return true
	}
	l.Elements = append(l.Elements, val)
	return true
}

func (v *VM) opContains(yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		if !yield(nil, fmt.Errorf("stack underflow during contains")) {
			return false
		}
		return true
	}
	container := v.pop()
	item := v.pop()

	if container == nil {
		if !yield(nil, fmt.Errorf("argument of type 'NoneType' is not iterable")) {
			return false
		}
		v.push(nil)
		return true
	}

	found := false
	switch c := container.(type) {
	case *List:
		for _, elem := range c.Elements {
			if elem == item {
				found = true
				break
			}
			// Fallback numeric check
			if i1, ok1 := ToInt64(elem); ok1 {
				if i2, ok2 := ToInt64(item); ok2 && i1 == i2 {
					found = true
					break
				}
			}
		}
	case []any:
		for _, elem := range c {
			if elem == item {
				found = true
				break
			}
			// Fallback numeric check
			if i1, ok1 := ToInt64(elem); ok1 {
				if i2, ok2 := ToInt64(item); ok2 && i1 == i2 {
					found = true
					break
				}
			}
		}
	case map[any]any:
		_, found = c[item]
	case map[string]any:
		if s, ok := item.(string); ok {
			_, found = c[s]
		}
	case string:
		if s, ok := item.(string); ok {
			found = strings.Contains(c, s)
		} else {
			if !yield(nil, fmt.Errorf("'in <string>' requires string as left operand, not %T", item)) {
				return false
			}
			v.push(nil)
			return true
		}
	case *Range:
		if i, ok := ToInt64(item); ok {
			found = c.Contains(i)
		}
	default:
		if !yield(nil, fmt.Errorf("argument of type '%T' is not iterable", container)) {
			return false
		}
		v.push(nil)
		return true
	}
	v.push(found)
	return true
}

func (v *VM) opUnpack(inst OpCode, yield func(*Interrupt, error) bool) bool {
	count := int(inst >> 8)
	if v.SP < 1 {
		if !yield(nil, fmt.Errorf("stack underflow during unpack")) {
			return false
		}
		return true
	}
	val := v.pop()

	var items []any

	switch t := val.(type) {
	case *List:
		items = t.Elements
	case []any:
		items = t
	case *Range:
		length := t.Len()
		items = make([]any, 0, length)
		curr := t.Start
		for range length {
			items = append(items, curr)
			curr += t.Step
		}
	case map[any]any:
		items = make([]any, 0, len(t))
		for k := range t {
			items = append(items, k)
		}
		// Sort keys to ensure deterministic unpacking
		allStrings := true
		for _, k := range items {
			if _, ok := k.(string); !ok {
				allStrings = false
				break
			}
		}
		if allStrings {
			sort.Slice(items, func(i, j int) bool {
				return items[i].(string) < items[j].(string)
			})
		}
	default:
		if !yield(nil, fmt.Errorf("cannot unpack %T", val)) {
			return false
		}
		return true
	}

	if len(items) != count {
		if !yield(nil, fmt.Errorf("unpack error: expected %d values, got %d", count, len(items))) {
			return false
		}
		return true
	}

	for i := count - 1; i >= 0; i-- {
		v.push(items[i])
	}
	return true
}

func (v *VM) opImport(inst OpCode, yield func(*Interrupt, error) bool) bool {
	idx := int(inst >> 8)
	nameObj := v.CurrentFun.Constants[idx]
	name, ok := nameObj.(string)
	if !ok {
		if !yield(nil, fmt.Errorf("import module name must be string")) {
			return false
		}
		v.push(nil)
		return true
	}

	if !yield(nil, fmt.Errorf("import not implemented: %s", name)) {
		return false
	}
	v.push(nil)
	return true
}

func (v *VM) opDefer() {
	val := v.pop()
	if d, ok := val.(*Closure); ok {
		v.Defers = append(v.Defers, d)
	}
}

func (v *VM) opAddrOf(inst OpCode, yield func(*Interrupt, error) bool) bool {
	idx := int(inst >> 8)
	name := v.CurrentFun.Constants[idx].(string)
	for e := v.Scope; e != nil; e = e.Parent {
		for _, vr := range e.Vars {
			if vr.Name == name {
				e.MarkCaptured()
				v.push(&Pointer{Target: e, Key: name})
				return true
			}
		}
	}
	if !yield(nil, fmt.Errorf("undefined variable: %s", name)) {
		return false
	}
	v.push(nil)
	return true
}

func (v *VM) opAddrOfIndex(yield func(*Interrupt, error) bool) bool {
	key := v.pop()
	target := v.pop()
	if target == nil {
		if !yield(nil, fmt.Errorf("indexing nil")) {
			return false
		}
		v.push(nil)
		return true
	}
	v.push(&Pointer{Target: target, Key: key})
	return true
}

func (v *VM) opAddrOfAttr(yield func(*Interrupt, error) bool) bool {
	name := v.pop()
	target := v.pop()
	if target == nil {
		if !yield(nil, fmt.Errorf("getattr on nil")) {
			return false
		}
		v.push(nil)
		return true
	}
	v.push(&Pointer{Target: target, Key: name})
	return true
}

func (v *VM) opDeref(yield func(*Interrupt, error) bool, depth int) bool {
	if depth > 100 {
		return yield(nil, fmt.Errorf("recursion depth limit exceeded"))
	}
	ptr := v.pop()
	if t, ok := ptr.(*Type); ok {
		v.push(&Type{
			Kind: KindPtr,
			Elem: t,
		})
		return true
	}
	if rt, ok := ptr.(reflect.Type); ok {
		v.push(&Type{
			Kind: KindPtr,
			Elem: FromReflectType(rt),
		})
		return true
	}
	p, ok := ptr.(*Pointer)
	if !ok {
		return yield(nil, fmt.Errorf("not a pointer: %T", ptr))
	}
	if p.ArrayType != nil {
		if list, ok := p.Target.(*List); ok {
			idx, _ := ToInt64(p.Key)
			n := p.ArrayType.Len
			rt := p.ArrayType.ToReflectType()
			if rt == nil {
				return yield(nil, fmt.Errorf("cannot deref array type %v", p.ArrayType))
			}
			arr := reflect.New(rt).Elem()
			for i := range n {
				arr.Index(i).Set(reflect.ValueOf(list.Elements[int(idx)+i]))
			}
			v.push(arr.Interface())
			return true
		}
	}
	if e, ok := p.Target.(*Env); ok {
		vr, ok := e.GetVar(p.Key.(string))
		if !ok {
			return yield(nil, fmt.Errorf("undefined variable: %s", p.Key))
		}
		v.push(vr.Val)
		return true
	}
	if p.Key == "" {
		v.push(p.Target)
		return true
	}
	if _, ok := p.Target.(*Struct); ok {
		v.push(p.Target)
		v.push(p.Key)
		return v.opGetAttr(yield, depth+1)
	}
	v.push(p.Target)
	v.push(p.Key)
	return v.opGetIndex(yield, depth+1)
}

func (v *VM) opSetDeref(yield func(*Interrupt, error) bool, depth int) bool {
	if depth > 100 {
		return yield(nil, fmt.Errorf("recursion depth limit exceeded"))
	}
	val := v.pop()
	ptr := v.pop()
	p, ok := ptr.(*Pointer)
	if !ok {
		return yield(nil, fmt.Errorf("not a pointer: %T", ptr))
	}
	if p.ArrayType != nil {
		if list, ok := p.Target.(*List); ok {
			idx, _ := ToInt64(p.Key)
			n := p.ArrayType.Len
			vval := reflect.ValueOf(val)
			for i := range n {
				list.Elements[int(idx)+i] = vval.Index(i).Interface()
			}
			return true
		}
	}
	if e, ok := p.Target.(*Env); ok {
		vr, _ := e.GetVar(p.Key.(string))
		e.DefWithType(p.Key.(string), val, vr.Type)
		return true
	}
	if _, ok := p.Target.(*Struct); ok {
		v.push(p.Target)
		v.push(p.Key)
		v.push(val)
		return v.opSetAttr(yield, depth+1)
	}
	v.push(p.Target)
	v.push(p.Key)
	v.push(val)
	return v.opSetIndex(yield, depth+1)
}

func (v *VM) opTypeAssert(yield func(*Interrupt, error) bool) bool {
	tObj := v.pop()
	val := v.pop()
	fail := func(err error) {
		v.IsPanicking = true
		v.PanicValue = err.Error()
		v.IP = len(v.CurrentFun.Code)
	}

	var dt *Type
	switch t := tObj.(type) {
	case *Type:
		dt = t
	case reflect.Type:
		dt = FromReflectType(t)
	}

	if dt == nil {
		fail(fmt.Errorf("invalid type for type assertion: %T", tObj))
		return true
	}

	if dt.Match(val) {
		v.push(val)
		return true
	}

	if val != nil {
		if dt.Kind == KindInterface || (dt.Kind == KindExternal && dt.External.Kind() == reflect.Interface) {
			if v.checkImplements(val, dt) {
				v.push(val)
				return true
			}
		}
		if s, ok := val.(*Struct); ok && (dt.Kind == KindStruct || (dt.Kind == KindExternal && dt.External.Kind() == reflect.Struct)) {
			if v.checkStructCompatible(s, dt) {
				v.push(val)
				return true
			}
		}
	}

	fail(fmt.Errorf("cannot convert %T to %v", val, dt.Name))
	return true
}

func (v *VM) opTypeAssertOk(yield func(*Interrupt, error) bool) bool {
	tObj := v.pop()
	val := v.pop()

	var targetType *Type
	switch t := tObj.(type) {
	case *Type:
		targetType = t
	case reflect.Type:
		targetType = FromReflectType(t)
	}

	if targetType == nil {
		v.push(nil)
		v.push(false)
		return true
	}

	if targetType.Match(val) {
		v.push(val)
		v.push(true)
		return true
	}

	if val != nil {
		if targetType.Kind == KindInterface || (targetType.Kind == KindExternal && targetType.External.Kind() == reflect.Interface) {
			if v.checkImplements(val, targetType) {
				v.push(val)
				v.push(true)
				return true
			}
		}
		if s, ok := val.(*Struct); ok && (targetType.Kind == KindStruct || (targetType.Kind == KindExternal && targetType.External.Kind() == reflect.Struct)) {
			if v.checkStructCompatible(s, targetType) {
				v.push(val)
				v.push(true)
				return true
			}
		}
	}

	v.push(targetType.Zero())
	v.push(false)
	return true
}

func (v *VM) getMapKeys(m map[any]any) []any {
	keys := make([]any, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	allStrings := true
	for _, k := range keys {
		if _, ok := k.(string); !ok {
			allStrings = false
			break
		}
	}
	if allStrings {
		sort.Slice(keys, func(i, j int) bool {
			return keys[i].(string) < keys[j].(string)
		})
	}
	return keys
}

func (v *VM) getMapStringKeys(m map[string]any) []any {
	keys := make([]any, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].(string) < keys[j].(string)
	})
	return keys
}

func (v *VM) newFuncIterator(fn any) *FuncIterator {
	it := &FuncIterator{}
	svm := NewVM(nil)
	it.InnerVM = svm
	yieldFunc := NativeFunc{
		Name: "yield",
		Func: func(svm *VM, args []any) (any, error) {
			if len(args) > 0 {
				it.K = args[0]
			}
			if len(args) > 1 {
				it.V = args[1]
			}
			it.Resumed = false
			return true, ErrYield
		},
	}
	svm.Def("yield", yieldFunc)
	svm.push(fn)
	svm.push(yieldFunc)
	svm.CurrentFun = &Function{
		Code: []OpCode{OpCall.With(1), OpReturn},
	}
	svm.IP = 0
	return it
}

func (v *VM) checkImplements(val any, t *Type) bool {
	if t == nil || (t.Kind != KindInterface && (t.Kind != KindExternal || t.External.Kind() != reflect.Interface)) {
		return false
	}
	if s, ok := val.(*Struct); ok {
		return v.checkStructImplements(s, t)
	}
	rv := reflect.ValueOf(val)
	for name := range t.Methods {
		m := rv.MethodByName(name)
		if !m.IsValid() {
			return false
		}
	}
	return true
}

func (v *VM) checkStructImplements(s *Struct, t *Type) bool {
	if t == nil || (t.Kind != KindInterface && (t.Kind != KindExternal || t.External.Kind() != reflect.Interface)) {
		return false
	}
	for name, targetType := range t.Methods {
		method := v.getStructMethod(s, name, 0)
		if method == nil {
			return false
		}
		if !v.checkMethodType(method, targetType) {
			return false
		}
	}
	return true
}

func (v *VM) checkStructCompatible(s *Struct, t *Type) bool {
	if t == nil || (t.Kind != KindStruct && (t.Kind != KindExternal || t.External.Kind() != reflect.Struct)) {
		return false
	}

	for _, f := range t.Fields {
		if _, ok := s.Fields[f.Name]; !ok {
			return false
		}
	}
	return true
}

func (v *VM) getStructMethod(s *Struct, name string, depth int) any {
	if depth > 100 {
		return nil
	}
	methodName := s.TypeName + "." + name
	ptrMethodName := "*" + s.TypeName + "." + name
	if method, ok := v.Get(methodName); ok {
		return method
	}
	if method, ok := v.Get(ptrMethodName); ok {
		return method
	}
	for _, emb := range s.Embedded {
		if fieldVal, ok := s.Fields[emb]; ok {
			if embStruct, ok := fieldVal.(*Struct); ok {
				if method := v.getStructMethod(embStruct, name, depth+1); method != nil {
					return method
				}
			}
		}
	}
	return nil
}

func (v *VM) checkMethodType(method any, target *Type) bool {
	var actual *Type
	switch m := method.(type) {
	case *Closure:
		actual = m.Fun.Type
	case NativeFunc:
		return true
	default:
		return false
	}
	if actual == nil {
		return true
	}
	if len(actual.In) != len(target.In)+1 {
		return false
	}
	if len(actual.Out) != len(target.Out) {
		return false
	}
	for i := 0; i < len(target.In); i++ {
		// Use String() for type identity check as it covers name and structure
		if actual.In[i+1].String() != target.In[i].String() {
			return false
		}
	}
	for i := 0; i < len(target.Out); i++ {
		if actual.Out[i].String() != target.Out[i].String() {
			return false
		}
	}
	return true
}

func (v *VM) handleUnwind(yield func(*Interrupt, error) bool) bool {
	// Execute defers for the current function
	if len(v.Defers) > 0 {
		last := len(v.Defers) - 1
		d := v.Defers[last]
		v.Defers = v.Defers[:last]
		// Push closure and call it. Unwinding continues after caller return.
		v.push(d)
		return v.opCall(OpCall.With(0), yield)
	}

	// Panic reached top level or all defers for frame are executed
	if len(v.CallStack) == 0 {
		err := fmt.Errorf("panic: %v", v.PanicValue)
		v.IsPanicking = false
		v.IP = len(v.CurrentFun.Code) // Ensure next loop exits
		yield(nil, err)
		return false
	}

	// Pop one frame and continue unwinding in the caller
	n := len(v.CallStack)
	frame := v.CallStack[n-1]
	v.CallStack = v.CallStack[:n-1]

	oldScope := v.Scope
	v.CurrentFun = frame.Fun
	v.IP = frame.ReturnIP
	v.Scope = frame.Env
	v.BP = frame.BP
	v.Defers = frame.Defers // Restore saved defers
	v.drop(v.SP - frame.BaseSP)
	v.freeEnv(oldScope)

	// If recovered during defer execution, resume normal return flow
	if !v.IsPanicking {
		v.push(nil) // return nil as a placeholder for recovered function result
		return true
	}

	// Still panicking. Force this function to exit to continue unwinding.
	v.IP = len(v.CurrentFun.Code)
	return true
}
