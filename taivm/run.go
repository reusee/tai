package taivm

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

func (v *VM) Run(yield func(*Interrupt, error) bool) {
	for {
		if v.IP < 0 || v.IP >= len(v.CurrentFun.Code) {
			if v.IsPanicking || len(v.CallStack) > 0 {
				if !v.handleUnwind(yield) {
					return
				}
				continue
			}
			return
		}

		inst := v.CurrentFun.Code[v.IP]
		v.IP++
		op := inst & 0xff

		switch op {
		case OpLoadConst:
			v.opLoadConst(inst)

		case OpLoadVar:
			if !v.opLoadVar(inst, yield) {
				return
			}

		case OpDefVar:
			v.opDefVar(inst)

		case OpSetVar:
			if !v.opSetVar(inst, yield) {
				return
			}

		case OpPop:
			v.opPop()

		case OpDup:
			if !v.opDup(yield) {
				return
			}

		case OpDup2:
			if !v.opDup2(yield) {
				return
			}

		case OpJump:
			v.opJump(inst)

		case OpJumpFalse:
			v.opJumpFalse(inst)

		case OpMakeClosure:
			v.opMakeClosure(inst)

		case OpCall:
			if !v.opCall(inst, yield) {
				return
			}

		case OpReturn:
			if !v.opReturn(yield) {
				return
			}

		case OpSuspend:
			if !yield(InterruptSuspend, nil) {
				return
			}

		case OpEnterScope:
			v.Scope = v.allocEnv(v.Scope)

		case OpLeaveScope:
			if v.Scope.Parent != nil {
				old := v.Scope
				v.Scope = v.Scope.Parent
				v.freeEnv(old)
			}

		case OpMakeList:
			if !v.opMakeList(inst, yield) {
				return
			}

		case OpMakeStruct:
			if !v.opMakeStruct(inst, yield) {
				return
			}

		case OpMakeMap:
			if !v.opMakeMap(inst, yield) {
				return
			}

		case OpGetIndex:
			if !v.opGetIndex(yield) {
				return
			}

		case OpSetIndex:
			if !v.opSetIndex(yield) {
				return
			}

		case OpSwap:
			if !v.opSwap(yield) {
				return
			}

		case OpGetLocal:
			v.opGetLocal(inst)

		case OpSetLocal:
			v.opSetLocal(inst)

		case OpDumpTrace:
			if !v.opDumpTrace(yield) {
				return
			}

		case OpBitAnd, OpBitOr, OpBitXor, OpBitLsh, OpBitRsh:
			if !v.opBitwise(op, yield) {
				return
			}

		case OpBitNot:
			if !v.opBitNot(yield) {
				return
			}

		case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpFloorDiv, OpPow:
			if !v.opMath(op, yield) {
				return
			}

		case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
			if !v.opCompare(op, yield) {
				return
			}

		case OpNot:
			if !v.opNot(yield) {
				return
			}

		case OpGetIter:
			if !v.opGetIter(yield) {
				return
			}

		case OpNextIter:
			if !v.opNextIter(inst, yield) {
				return
			}

		case OpMakeTuple:
			if !v.opMakeTuple(inst, yield) {
				return
			}

		case OpGetSlice:
			if !v.opGetSlice(yield) {
				return
			}

		case OpSetSlice:
			if !v.opSetSlice(yield) {
				return
			}

		case OpGetAttr:
			if !v.opGetAttr(yield) {
				return
			}

		case OpSetAttr:
			if !v.opSetAttr(yield) {
				return
			}

		case OpCallKw:
			if !v.opCallKw(inst, yield) {
				return
			}

		case OpListAppend:
			if !v.opListAppend(yield) {
				return
			}

		case OpContains:
			if !v.opContains(yield) {
				return
			}

		case OpUnpack:
			if !v.opUnpack(inst, yield) {
				return
			}

		case OpImport:
			if !v.opImport(inst, yield) {
				return
			}

		case OpDefer:
			v.opDefer()

		case OpAddrOf:
			if !v.opAddrOf(inst, yield) {
				return
			}

		case OpAddrOfIndex:
			if !v.opAddrOfIndex(yield) {
				return
			}

		case OpAddrOfAttr:
			if !v.opAddrOfAttr(yield) {
				return
			}

		case OpDeref:
			if !v.opDeref(yield) {
				return
			}

		case OpSetDeref:
			if !v.opSetDeref(yield) {
				return
			}

		case OpTypeAssert:
			if !v.opTypeAssert(yield) {
				return
			}

		case OpTypeAssertOk:
			if !v.opTypeAssertOk(yield) {
				return
			}
		}

	}
}

func ToInt64(v any) (int64, bool) {
	switch i := v.(type) {
	case int:
		return int64(i), true
	case int8:
		return int64(i), true
	case int16:
		return int64(i), true
	case int32:
		return int64(i), true
	case int64:
		return i, true
	case uint:
		return int64(i), true
	case uint8:
		return int64(i), true
	case uint16:
		return int64(i), true
	case uint32:
		return int64(i), true
	case uint64:
		return int64(i), true
	}
	return 0, false
}

func ToFloat64(v any) (float64, bool) {
	switch i := v.(type) {
	case float64:
		return i, true
	case float32:
		return float64(i), true
	case int:
		return float64(i), true
	case int8:
		return float64(i), true
	case int16:
		return float64(i), true
	case int32:
		return float64(i), true
	case int64:
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

func isFloat(v any) bool {
	switch v.(type) {
	case float32, float64:
		return true
	}
	return false
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
	case string:
		return i == ""
	case nil:
		return true
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

func (v *VM) opLoadConst(inst OpCode) {
	idx := int(inst >> 8)
	if v.SP >= len(v.OperandStack) {
		v.growOperandStack()
	}
	v.OperandStack[v.SP] = v.CurrentFun.Constants[idx]
	v.SP++
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
	idx := int(inst >> 8)
	name := v.CurrentFun.Constants[idx].(string)
	v.Scope.Def(name, v.pop())
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

func (v *VM) opPop() {
	if v.SP > 0 {
		v.SP--
		v.OperandStack[v.SP] = nil
	}
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

func (v *VM) opJump(inst OpCode) {
	offset := int(int32(inst) >> 8)
	v.IP += offset
}

func (v *VM) opJumpFalse(inst OpCode) {
	offset := int(int32(inst) >> 8)
	var val any
	if v.SP > 0 {
		v.SP--
		val = v.OperandStack[v.SP]
		v.OperandStack[v.SP] = nil
	}
	if isZero(val) {
		v.IP += offset
	}
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

	// Callee is below args on the stack
	calleeIdx := v.SP - argc - 1
	callee := v.OperandStack[calleeIdx]

	switch fn := callee.(type) {
	case *Closure:
		return v.callClosure(fn, argc, calleeIdx, yield)

	case *BoundMethod:
		if v.SP >= len(v.OperandStack) {
			v.growOperandStack()
		}
		// Shift arguments
		copy(v.OperandStack[calleeIdx+2:v.SP+1], v.OperandStack[calleeIdx+1:v.SP])
		v.SP++
		v.OperandStack[calleeIdx] = fn.Fun
		v.OperandStack[calleeIdx+1] = fn.Receiver
		return v.opCall(OpCall.With(argc+1), yield)

	case NativeFunc:
		return v.callNative(fn, argc, calleeIdx, yield)

	case reflect.Type: // Handle type conversion calls: T(x)
		return v.callTypeConversion(fn, argc, calleeIdx, yield)

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

	locals := make([]any, numParams)

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
			newCap := len(v.OperandStack) * 2
			if newCap < needed {
				newCap = needed
			}
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
			newCap := len(v.OperandStack) * 2
			if newCap < needed {
				newCap = needed
			}
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
	// Zero-allocation slice view of arguments
	// Note: Slice is valid only until next Stack modification, which is fine for sync calls
	args := v.OperandStack[calleeIdx+1 : v.SP]
	res, err := fn.Func(v, args)

	if err != nil {
		if v.IsPanicking {
			v.IP = len(v.CurrentFun.Code) // ensure we transition to unwinding immediately
			return true
		}
		if !yield(nil, err) {
			return false
		}
		res = nil
	}
	v.OperandStack[calleeIdx] = res
	for i := calleeIdx + 1; i < v.SP; i++ {
		v.OperandStack[i] = nil
	}
	v.SP = calleeIdx + 1
	return true
}

func (v *VM) callTypeConversion(t reflect.Type, argc, calleeIdx int, yield func(*Interrupt, error) bool) bool {
	if argc != 1 {
		if !yield(nil, fmt.Errorf("type conversion expects 1 argument")) {
			return false
		}
		v.drop(argc + 1)
		v.push(nil)
		return true
	}
	arg := v.OperandStack[calleeIdx+1]
	var res any
	if arg == nil {
		res = reflect.Zero(t).Interface()
	} else {
		val := reflect.ValueOf(arg)
		if val.Type().ConvertibleTo(t) {
			res = val.Convert(t).Interface()
		} else {
			if !yield(nil, fmt.Errorf("cannot convert %T to %v", arg, t)) {
				return false
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
	v.push(&Struct{TypeName: typeNameStr, Fields: m, Embedded: embedded})
	return true
}

func (v *VM) opGetIndex(yield func(*Interrupt, error) bool) bool {
	key := v.pop()
	target := v.pop()
	if target == nil {
		if !yield(nil, fmt.Errorf("indexing nil")) {
			return false
		}
		v.push(nil)
		return true
	}

	var val any
	switch t := target.(type) {
	case *List:
		var idx int
		var ok bool
		switch i := key.(type) {
		case int:
			idx = i
			ok = true
		case int64:
			idx = int(i)
			ok = true
		}

		if !ok {
			if !yield(nil, fmt.Errorf("list index must be int, got %T", key)) {
				return false
			}
			val = nil
		} else if idx < 0 || idx >= len(t.Elements) {
			if !yield(nil, fmt.Errorf("index out of bounds: %d", idx)) {
				return false
			}
			val = nil
		} else {
			val = t.Elements[idx]
		}

	case []any:
		// Backward compatibility for native code returning []any
		var idx int
		var ok bool
		switch i := key.(type) {
		case int:
			idx = i
			ok = true
		case int64:
			idx = int(i)
			ok = true
		}

		if !ok {
			if !yield(nil, fmt.Errorf("slice index must be int, got %T", key)) {
				return false
			}
			val = nil
		} else if idx < 0 || idx >= len(t) {
			if !yield(nil, fmt.Errorf("index out of bounds: %d", idx)) {
				return false
			}
			val = nil
		} else {
			val = t[idx]
		}

	case map[any]any:
		val = t[key]

	case map[string]any:
		if k, ok := key.(string); ok {
			val = t[k]
		}

	case *Range:
		var idx int64
		var ok bool
		switch i := key.(type) {
		case int:
			idx = int64(i)
			ok = true
		case int64:
			idx = i
			ok = true
		}
		if !ok {
			if !yield(nil, fmt.Errorf("range index must be int, got %T", key)) {
				return false
			}
			val = nil
		} else {
			length := t.Len()
			if idx < 0 {
				idx += length
			}
			if idx < 0 || idx >= length {
				if !yield(nil, fmt.Errorf("index out of bounds: %d", idx)) {
					return false
				}
				val = nil
			} else {
				val = t.Start + idx*t.Step
			}
		}

	default:
		if !yield(nil, fmt.Errorf("type %T is not indexable", target)) {
			return false
		}
	}
	v.push(val)
	return true
}

func (v *VM) opSetIndex(yield func(*Interrupt, error) bool) bool {
	val := v.pop()
	key := v.pop()
	target := v.pop()

	if target == nil {
		if !yield(nil, fmt.Errorf("assignment to nil")) {
			return false
		}
		return true
	}

	switch t := target.(type) {
	case *List:
		if t.Immutable {
			if !yield(nil, fmt.Errorf("tuple is immutable")) {
				return false
			}
			return true
		}
		var idx int
		var ok bool
		switch i := key.(type) {
		case int:
			idx = i
			ok = true
		case int64:
			idx = int(i)
			ok = true
		}
		if !ok {
			if !yield(nil, fmt.Errorf("list index must be int, got %T", key)) {
				return false
			}
			return true
		}
		if idx < 0 || idx >= len(t.Elements) {
			if !yield(nil, fmt.Errorf("index out of bounds: %d", idx)) {
				return false
			}
			return true
		}
		t.Elements[idx] = val

	case []any:
		// Backward compatibility
		var idx int
		var ok bool
		switch i := key.(type) {
		case int:
			idx = i
			ok = true
		case int64:
			idx = int(i)
			ok = true
		}
		if !ok {
			if !yield(nil, fmt.Errorf("slice index must be int, got %T", key)) {
				return false
			}
			return true
		}
		if idx < 0 || idx >= len(t) {
			if !yield(nil, fmt.Errorf("index out of bounds: %d", idx)) {
				return false
			}
			return true
		}
		t[idx] = val

	case map[any]any:
		t[key] = val

	case map[string]any:
		if k, ok := key.(string); ok {
			t[k] = val
		} else {
			if !yield(nil, fmt.Errorf("map key must be string, got %T", key)) {
				return false
			}
		}

	default:
		if !yield(nil, fmt.Errorf("type %T does not support assignment", target)) {
			return false
		}
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

func (v *VM) opGetLocal(inst OpCode) {
	idx := int(inst >> 8)
	if v.SP >= len(v.OperandStack) {
		v.growOperandStack()
	}
	v.OperandStack[v.SP] = v.OperandStack[v.BP+idx]
	v.SP++
}

func (v *VM) opSetLocal(inst OpCode) {
	idx := int(inst >> 8)
	var val any
	if v.SP > 0 {
		v.SP--
		val = v.OperandStack[v.SP]
		v.OperandStack[v.SP] = nil
	}
	v.OperandStack[v.BP+idx] = val
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
		if !yield(nil, fmt.Errorf("stack underflow during bitwise op")) {
			return false
		}
		return true
	}
	b := v.pop()
	a := v.pop()

	if op == OpBitOr {
		m1, ok1 := a.(map[any]any)
		m2, ok2 := b.(map[any]any)
		if ok1 && ok2 {
			newMap := make(map[any]any, len(m1)+len(m2))
			for k, val := range m1 {
				newMap[k] = val
			}
			for k, val := range m2 {
				newMap[k] = val
			}
			v.push(newMap)
			return true
		}
	}

	if res, ok, err := bitwiseSameType(op, a, b); ok {
		if err != nil {
			if !yield(nil, err) {
				return false
			}
			v.push(nil)
			return true
		}
		v.push(res)
		return true
	}

	i1, ok1 := ToInt64(a)
	i2, ok2 := ToInt64(b)
	if !ok1 || !ok2 {
		if !yield(nil, fmt.Errorf("bitwise operands must be integers, got %T and %T", a, b)) {
			return false
		}
		v.push(nil)
		return true
	}
	var res int64
	switch op {
	case OpBitAnd:
		res = i1 & i2
	case OpBitOr:
		res = i1 | i2
	case OpBitXor:
		res = i1 ^ i2
	case OpBitLsh:
		if i2 < 0 {
			if !yield(nil, fmt.Errorf("negative shift count: %d", i2)) {
				return false
			}
			v.push(nil)
			return true
		}
		res = i1 << uint(i2)
	case OpBitRsh:
		if i2 < 0 {
			if !yield(nil, fmt.Errorf("negative shift count: %d", i2)) {
				return false
			}
			v.push(nil)
			return true
		}
		res = i1 >> uint(i2)
	}
	v.push(res)
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
	default:
		if !yield(nil, fmt.Errorf("bitwise not operand must be int, got %T", a)) {
			return false
		}
		v.push(nil)
		return true
	}
	v.push(res)
	return true
}

func (v *VM) opMath(op OpCode, yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		if !yield(nil, fmt.Errorf("stack underflow during math op")) {
			return false
		}
		return true
	}
	b := v.pop()
	a := v.pop()

	if op == OpAdd {
		s1, ok1 := a.(string)
		s2, ok2 := b.(string)
		if ok1 && ok2 {
			v.push(s1 + s2)
			return true
		}

		l1, ok1 := a.(*List)
		l2, ok2 := b.(*List)
		if ok1 && ok2 {
			newElems := make([]any, 0, len(l1.Elements)+len(l2.Elements))
			newElems = append(newElems, l1.Elements...)
			newElems = append(newElems, l2.Elements...)
			v.push(&List{Elements: newElems, Immutable: false})
			return true
		}
	}

	if res, ok, err := arithmeticSameType(op, a, b); ok {
		if err != nil {
			if !yield(nil, err) {
				return false
			}
			v.push(nil)
			return true
		}
		v.push(res)
		return true
	}

	if isComplex(a) || isComplex(b) {
		c1, ok1 := toComplex128(a)
		c2, ok2 := toComplex128(b)
		if !ok1 || !ok2 {
			if !yield(nil, fmt.Errorf("invalid operands for complex math: %T, %T", a, b)) {
				return false
			}
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
				if !yield(nil, fmt.Errorf("division by zero")) {
					return false
				}
				v.push(nil)
				return true
			}
			res = c1 / c2
		case OpPow:
			if !yield(nil, fmt.Errorf("complex pow not supported")) {
				return false
			}
			v.push(nil)
			return true
		default:
			if !yield(nil, fmt.Errorf("unsupported operation for complex numbers")) {
				return false
			}
			v.push(nil)
			return true
		}
		v.push(res)
		return true
	}

	if isFloat(a) || isFloat(b) {
		f1, ok1 := ToFloat64(a)
		f2, ok2 := ToFloat64(b)
		if !ok1 || !ok2 {
			if !yield(nil, fmt.Errorf("invalid operands for float math: %T, %T", a, b)) {
				return false
			}
			v.push(nil)
			return true
		}
		var res float64
		switch op {
		case OpAdd:
			res = f1 + f2
		case OpSub:
			res = f1 - f2
		case OpMul:
			res = f1 * f2
		case OpDiv:
			if f2 == 0 {
				if !yield(nil, fmt.Errorf("division by zero")) {
					return false
				}
				v.push(nil)
				return true
			}
			res = f1 / f2
		case OpFloorDiv:
			if f2 == 0 {
				if !yield(nil, fmt.Errorf("division by zero")) {
					return false
				}
				v.push(nil)
				return true
			}
			res = math.Floor(f1 / f2)
		case OpPow:
			res = math.Pow(f1, f2)
		default:
			if !yield(nil, fmt.Errorf("unsupported operation for floats")) {
				return false
			}
			v.push(nil)
			return true
		}
		v.push(res)
		return true
	}

	i1, ok1 := ToInt64(a)
	i2, ok2 := ToInt64(b)
	if !ok1 || !ok2 {
		if !yield(nil, fmt.Errorf("math operands must be numeric, got %T and %T", a, b)) {
			return false
		}
		v.push(nil)
		return true
	}

	var res int64
	switch op {
	case OpAdd:
		res = i1 + i2
	case OpSub:
		res = i1 - i2
	case OpMul:
		res = i1 * i2
	case OpDiv:
		if i2 == 0 {
			if !yield(nil, fmt.Errorf("division by zero")) {
				return false
			}
			v.push(nil)
			return true
		}
		res = i1 / i2
	case OpMod:
		if i2 == 0 {
			if !yield(nil, fmt.Errorf("division by zero")) {
				return false
			}
			v.push(nil)
			return true
		}
		res = i1 % i2
		if (res < 0) != (i2 < 0) && res != 0 {
			res += i2
		}
	case OpFloorDiv:
		if i2 == 0 {
			if !yield(nil, fmt.Errorf("division by zero")) {
				return false
			}
			v.push(nil)
			return true
		}
		res = i1 / i2
		if (i1 < 0) != (i2 < 0) && i1%i2 != 0 {
			res--
		}
	case OpPow:
		if i2 < 0 {
			resFloat := math.Pow(float64(i1), float64(i2))
			v.push(resFloat)
			return true
		}
		var overflow bool
		res, overflow = intPow(i1, i2)
		if overflow {
			resFloat := math.Pow(float64(i1), float64(i2))
			v.push(resFloat)
			return true
		}
	}
	v.push(res)
	return true
}

func intPow(base, exp int64) (int64, bool) {
	if exp == 0 {
		return 1, false
	}
	if exp < 0 {
		return 0, false
	}
	if base == 0 {
		return 0, false
	}
	if base == 1 {
		return 1, false
	}
	if base == -1 {
		if exp%2 == 0 {
			return 1, false
		}
		return -1, false
	}

	const maxInt64 = math.MaxInt64
	const minInt64 = math.MinInt64

	result := int64(1)
	currentBase := base
	currentExp := exp

	for currentExp > 0 {
		if currentExp&1 == 1 {
			// Check overflow before multiplication
			if result > 0 && currentBase > 0 && result > maxInt64/currentBase {
				return 0, true
			}
			if result > 0 && currentBase < 0 && currentBase < minInt64/result {
				return 0, true
			}
			if result < 0 && currentBase > 0 && result < minInt64/currentBase {
				return 0, true
			}
			if result < 0 && currentBase < 0 && result < maxInt64/currentBase {
				return 0, true
			}

			newResult := result * currentBase
			if currentBase != 0 && newResult/currentBase != result {
				return 0, true
			}
			result = newResult
		}

		currentExp >>= 1
		if currentExp > 0 {
			newBase := currentBase * currentBase
			if currentBase != 0 && currentBase != 1 && currentBase != -1 {
				if newBase/currentBase != currentBase {
					return 0, true
				}
			}
			currentBase = newBase
		}
	}

	return result, false
}

func (v *VM) opCompare(op OpCode, yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		if !yield(nil, fmt.Errorf("stack underflow during comparison")) {
			return false
		}
		return true
	}
	b := v.pop()
	a := v.pop()
	match := a == b
	if !match {
		i1, ok1 := ToInt64(a)
		i2, ok2 := ToInt64(b)
		if ok1 && ok2 {
			match = i1 == i2
		} else {
			f1, ok1 := ToFloat64(a)
			f2, ok2 := ToFloat64(b)
			if ok1 && ok2 {
				match = f1 == f2
			}
		}
	}

	switch op {
	case OpEq:
		v.push(match)
		return true
	case OpNe:
		v.push(!match)
		return true
	}

	if s1, ok := a.(string); ok {
		if s2, ok := b.(string); ok {
			var res bool
			switch op {
			case OpLt:
				res = s1 < s2
			case OpLe:
				res = s1 <= s2
			case OpGt:
				res = s1 > s2
			case OpGe:
				res = s1 >= s2
			}
			v.push(res)
			return true
		}
	}

	if isComplex(a) || isComplex(b) {
		if !yield(nil, fmt.Errorf("complex numbers are not ordered")) {
			return false
		}
		v.push(nil)
		return true
	}

	if isFloat(a) || isFloat(b) {
		f1, ok1 := ToFloat64(a)
		f2, ok2 := ToFloat64(b)
		if !ok1 || !ok2 {
			if !yield(nil, fmt.Errorf("invalid operands for float comparison: %T, %T", a, b)) {
				return false
			}
			v.push(nil)
			return true
		}
		var res bool
		switch op {
		case OpLt:
			res = f1 < f2
		case OpLe:
			res = f1 <= f2
		case OpGt:
			res = f1 > f2
		case OpGe:
			res = f1 >= f2
		}
		v.push(res)
		return true
	}

	i1, ok1 := ToInt64(a)
	i2, ok2 := ToInt64(b)
	if ok1 && ok2 {
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
		}
		v.push(res)
		return true
	}

	if !yield(nil, fmt.Errorf("unsupported type for comparison: %T vs %T", a, b)) {
		return false
	}
	v.push(nil)
	return true
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
		keys := make([]any, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		// Try to sort if all keys are strings to ensure deterministic iteration
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
		v.push(&MapIterator{Keys: keys})
	case *Range:
		v.push(&RangeIterator{
			Range: t,
			Curr:  t.Start,
		})
	default:
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
	iter := v.OperandStack[v.SP-1] // Peek iterator

	switch it := iter.(type) {
	case *ListIterator:
		if it.Idx < len(it.List.Elements) {
			v.push(it.List.Elements[it.Idx])
			it.Idx++
		} else {
			v.pop() // pop iterator
			v.IP += offset
		}
	case *MapIterator:
		if it.Idx < len(it.Keys) {
			v.push(it.Keys[it.Idx])
			it.Idx++
		} else {
			v.pop() // pop iterator
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

func (v *VM) opGetAttr(yield func(*Interrupt, error) bool) bool {
	if v.SP < 2 {
		if !yield(nil, fmt.Errorf("stack underflow during getattr")) {
			return false
		}
		return true
	}
	name := v.pop()
	target := v.pop()

	nameStr, ok := name.(string)
	if !ok {
		if !yield(nil, fmt.Errorf("attribute name must be string, got %T", name)) {
			return false
		}
		v.push(nil)
		return true
	}

	if target == nil {
		if !yield(nil, fmt.Errorf("getattr on nil")) {
			return false
		}
		v.push(nil)
		return true
	}

	switch t := target.(type) {
	case *Struct:
		if val, ok := t.Fields[nameStr]; ok {
			v.push(val)
			return true
		}
		if t.TypeName != "" {
			ptrMethodName := "*" + t.TypeName + "." + nameStr
			if method, ok := v.Get(ptrMethodName); ok {
				v.push(&BoundMethod{
					Receiver: &Pointer{Target: t, Key: ""},
					Fun:      method,
				})
				return true
			}
			methodName := t.TypeName + "." + nameStr
			if method, ok := v.Get(methodName); ok {
				v.push(&BoundMethod{
					Receiver: t,
					Fun:      method,
				})
				return true
			}
		}
		// Search promoted attributes recursively
		for _, emb := range t.Embedded {
			if fieldVal, ok := t.Fields[emb]; ok && fieldVal != nil {
				v.push(fieldVal)
				v.push(name)
				if v.opGetAttr(yield) {
					res := v.OperandStack[v.SP-1]
					if res != nil {
						return true
					}
					v.pop()
				}
			}
		}
		if !yield(nil, fmt.Errorf("struct has no field or method '%s'", nameStr)) {
			return false
		}
		v.push(nil)
		return true

	case *List:
		if nameStr == "append" {
			v.push(&BoundMethod{
				Receiver: t,
				Fun: NativeFunc{
					Name: "list.append",
					Func: ListAppend,
				},
			})
			return true
		}
		if !yield(nil, fmt.Errorf("list has no attribute '%s'", nameStr)) {
			return false
		}
		v.push(nil)
		return true

	case *Pointer:
		v.push(t.Target)
		v.push(name)
		return v.opGetAttr(yield)

	default:
		if !yield(nil, fmt.Errorf("type %T has no attributes", target)) {
			return false
		}
		v.push(nil)
	}
	return true
}

func (v *VM) opSetAttr(yield func(*Interrupt, error) bool) bool {
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
		if _, ok := t.Fields[nameStr]; ok {
			t.Fields[nameStr] = val
			return true
		}
		// Try to set promoted field
		for _, emb := range t.Embedded {
			if fieldVal, ok := t.Fields[emb]; ok && fieldVal != nil {
				v.push(fieldVal)
				v.push(name)
				v.push(val)
				if v.opSetAttr(yield) {
					return true
				}
			}
		}
		if t.Fields == nil {
			t.Fields = make(map[string]any)
		}
		t.Fields[nameStr] = val

	case *Pointer:
		v.push(t.Target)
		v.push(name)
		v.push(val)
		return v.opSetAttr(yield)

	default:
		if !yield(nil, fmt.Errorf("type %T does not support attribute assignment", target)) {
			return false
		}
	}
	return true
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
			newCap := len(v.OperandStack) * 2
			if newCap < needed {
				newCap = needed
			}
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
		res, err := fn.Func(v, posArgs)
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
		for i := int64(0); i < length; i++ {
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

func (v *VM) opDeref(yield func(*Interrupt, error) bool) bool {
	ptr := v.pop()
	p, ok := ptr.(*Pointer)
	if !ok {
		if !yield(nil, fmt.Errorf("not a pointer: %T", ptr)) {
			return false
		}
		v.push(nil)
		return true
	}
	if e, ok := p.Target.(*Env); ok {
		name := p.Key.(string)
		val, ok := e.Get(name)
		if !ok {
			if !yield(nil, fmt.Errorf("undefined variable: %s", name)) {
				return false
			}
			v.push(nil)
			return true
		}
		v.push(val)
		return true
	}
	// Handle pointer to entire struct (empty Key)
	if p.Key == "" {
		v.push(p.Target)
		return true
	}
	v.push(p.Target)
	v.push(p.Key)
	if _, ok := p.Target.(*Struct); ok {
		return v.opGetAttr(yield)
	}
	return v.opGetIndex(yield)
}

func (v *VM) opSetDeref(yield func(*Interrupt, error) bool) bool {
	val := v.pop()
	ptr := v.pop()
	p, ok := ptr.(*Pointer)
	if !ok {
		if !yield(nil, fmt.Errorf("not a pointer: %T", ptr)) {
			return false
		}
		return true
	}
	if e, ok := p.Target.(*Env); ok {
		e.Def(p.Key.(string), val)
		return true
	}
	v.push(p.Target)
	v.push(p.Key)
	v.push(val)
	if _, ok := p.Target.(*Struct); ok {
		return v.opSetAttr(yield)
	}
	return v.opSetIndex(yield)
}

func (v *VM) opTypeAssert(yield func(*Interrupt, error) bool) bool {
	tObj := v.pop()
	val := v.pop()
	t, ok := tObj.(reflect.Type)
	if !ok {
		if !yield(nil, fmt.Errorf("invalid type for type assertion: %T", tObj)) {
			return false
		}
		v.push(nil)
		return true
	}
	fail := func(err error) {
		v.IsPanicking = true
		v.PanicValue = err.Error()
		v.IP = len(v.CurrentFun.Code) // ensure we transition to unwinding immediately
	}
	if val == nil {
		fail(fmt.Errorf("interface is nil, not %v", t))
		return true
	}
	if t.Kind() == reflect.Interface {
		if s, ok := val.(*Struct); ok {
			if v.checkStructImplements(s, t) {
				v.push(val)
				return true
			}
		} else if reflect.TypeOf(val).Implements(t) {
			v.push(val)
			return true
		}
		fail(fmt.Errorf("cannot convert %T to %v", val, t))
		return true
	}
	valType := reflect.TypeOf(val)
	if valType.AssignableTo(t) {
		v.push(val)
		return true
	}
	if s, ok := val.(*Struct); ok {
		if t.Kind() == reflect.Ptr || t.Kind() == reflect.Struct {
			if s.TypeName == t.Name() {
				v.push(val)
				return true
			}
		}
	}
	fail(fmt.Errorf("cannot convert %T to %v", val, t))
	return true
}

func (v *VM) opTypeAssertOk(yield func(*Interrupt, error) bool) bool {
	tObj := v.pop()
	val := v.pop()
	t, ok := tObj.(reflect.Type)
	if !ok {
		v.push(nil)
		v.push(false)
		return true
	}
	if val == nil {
		v.push(reflect.Zero(t).Interface())
		v.push(false)
		return true
	}
	if t.Kind() == reflect.Interface {
		if s, ok := val.(*Struct); ok {
			if v.checkStructImplements(s, t) {
				v.push(val)
				v.push(true)
				return true
			}
		} else if reflect.TypeOf(val).Implements(t) {
			v.push(val)
			v.push(true)
			return true
		}
		v.push(reflect.Zero(t).Interface())
		v.push(false)
		return true
	}
	valType := reflect.TypeOf(val)
	if valType.AssignableTo(t) {
		v.push(val)
		v.push(true)
		return true
	}
	if s, ok := val.(*Struct); ok {
		if t.Kind() == reflect.Ptr || t.Kind() == reflect.Struct {
			if s.TypeName == t.Name() {
				v.push(val)
				v.push(true)
				return true
			}
		}
	}
	v.push(reflect.Zero(t).Interface())
	v.push(false)
	return true
}

func (v *VM) checkStructImplements(s *Struct, it reflect.Type) bool {
	for i := 0; i < it.NumMethod(); i++ {
		m := it.Method(i)
		if !v.structHasMethod(s, m.Name) {
			return false
		}
	}
	return true
}

func (v *VM) structHasMethod(s *Struct, name string) bool {
	methodName := s.TypeName + "." + name
	ptrMethodName := "*" + s.TypeName + "." + name
	if _, ok := v.Get(methodName); ok {
		return true
	}
	if _, ok := v.Get(ptrMethodName); ok {
		return true
	}
	for _, emb := range s.Embedded {
		if fieldVal, ok := s.Fields[emb]; ok {
			if embStruct, ok := fieldVal.(*Struct); ok {
				if v.structHasMethod(embStruct, name) {
					return true
				}
			}
		}
	}
	return false
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
