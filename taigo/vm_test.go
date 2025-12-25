package taigo

import (
	"strings"
	"testing"

	"github.com/reusee/tai/taivm"
)

func runVM(t *testing.T, src string) *taivm.VM {
	vm, err := NewVM("main", strings.NewReader(src))
	if err != nil {
		t.Helper()
		t.Fatal(err)
	}
	vm.Run(func(i *taivm.Interrupt, err error) bool {
		if err != nil {
			t.Helper()
			t.Fatalf("vm error: %v", err)
		}
		return true
	})
	return vm
}

func checkInt(t *testing.T, vm *taivm.VM, name string, expected int64) {
	val, ok := vm.Get(name)
	if !ok {
		t.Helper()
		t.Fatalf("variable %s not found", name)
	}
	if i, ok := taivm.ToInt64(val); !ok || i != expected {
		t.Helper()
		t.Fatalf("variable %s: expected %d, got %v", name, expected, val)
	}
}

func checkString(t *testing.T, vm *taivm.VM, name string, expected string) {
	val, ok := vm.Get(name)
	if !ok {
		t.Helper()
		t.Fatalf("variable %s not found", name)
	}
	if s, ok := val.(string); !ok || s != expected {
		t.Helper()
		t.Fatalf("variable %s: expected %q, got %v", name, expected, val)
	}
}

func checkBool(t *testing.T, vm *taivm.VM, name string, expected bool) {
	val, ok := vm.Get(name)
	if !ok {
		t.Helper()
		t.Fatalf("variable %s not found", name)
	}
	if b, ok := val.(bool); !ok || b != expected {
		t.Helper()
		t.Fatalf("variable %s: expected %v, got %v", name, expected, val)
	}
}

func TestVMBasicLiterals(t *testing.T) {
	vm := runVM(t, `
		package main
		var a = 1
		var b = -5
		var c = "hello"
		var d = true
	`)
	checkInt(t, vm, "a", 1)
	checkInt(t, vm, "b", -5)
	checkString(t, vm, "c", "hello")
	checkBool(t, vm, "d", true)
}

func TestVMArithmetic(t *testing.T) {
	vm := runVM(t, `
		package main
		var a = 1 + 2
		var b = 10 - 4
		var c = 2 * 3
		var d = 15 / 3
		var e = 10 % 3
		var f = 0
		func init() {
			f += 5
			f *= 2
		}
	`)
	checkInt(t, vm, "a", 3)
	checkInt(t, vm, "b", 6)
	checkInt(t, vm, "c", 6)
	checkInt(t, vm, "d", 5)
	checkInt(t, vm, "e", 1)
	checkInt(t, vm, "f", 10)
}

func TestVMComparisonAndLogic(t *testing.T) {
	vm := runVM(t, `
		package main

		var res = 0
		func init() {
			if 1 < 2 { res = 1 }
			if 2 > 1 { res += 1 }
			if 1 == 1 { res += 1 }
			if 1 != 2 { res += 1 }
		}
		
		var neg = 0
		func init() {
			if !false { neg = 1 }
		}

		var bit = 0
		func init() {
			bit = 1 | 2 // 3
			bit = bit & 2 // 2
			bit = bit ^ 1 // 3
		}
	`)
	checkInt(t, vm, "res", 4)
	checkInt(t, vm, "neg", 1)
	checkInt(t, vm, "bit", 3)
}

func TestVMSwitch(t *testing.T) {
	vm := runVM(t, `
		package main

		var a = 1
		var res = 0
		func init() {
			switch a {
			case 1:
				res = 10
			case 2:
				res = 20
			default:
				res = 30
			}
		}

		var res2 = 0
		func init() {
			switch {
			case a == 1:
				res2 = 100
			default:
				res2 = 200
			}
		}
	`)
	checkInt(t, vm, "res", 10)
	checkInt(t, vm, "res2", 100)
}

func TestVMFunctions(t *testing.T) {
	vm := runVM(t, `
		package main

		func add(a, b any) {
			return a + b
		}
		var res = add(3, 4)

		func fact(n any) {
			if n <= 1 { return 1 }
			return n * fact(n-1)
		}
		var f = fact(5)

		func multi() {
			return 1, 2
		}
		var x, y = multi()
	`)
	checkInt(t, vm, "res", 7)
	checkInt(t, vm, "f", 120)
	checkInt(t, vm, "x", 1)
	checkInt(t, vm, "y", 2)
}

func TestVMClosures(t *testing.T) {
	vm := runVM(t, `
		package main
		func makeAdder(base any) {
			return func(v any) {
				return base + v
			}
		}
		var add5 = makeAdder(5)
		var res = add5(10)
	`)
	checkInt(t, vm, "res", 15)
}

func TestVMMultiAssignment(t *testing.T) {
	vm := runVM(t, `
		package main
		var a, b = 1, 2
		func init() {
			a, b = b, a
		}
	`)
	checkInt(t, vm, "a", 2)
	checkInt(t, vm, "b", 1)
}

func TestVMSlices(t *testing.T) {
	vm := runVM(t, `
		package main

		var s = make("[]int", 3)
		func init() {
			s[0] = 10
			s[1] = 20
			s[2] = 30
		}

		var l = len(s)
		var c = cap(s)
		var last int
		func init() {
			s = append(s, 40)
			last = s[3]
		}
		
		var part = s[1:3]
	`)
	checkInt(t, vm, "l", 3)
	checkInt(t, vm, "last", 40)

	val, _ := vm.Get("part")
	if _, ok := val.(*taivm.List); !ok {
		t.Errorf("expected *List for slice result, got %T", val)
	}
}

func TestVMMaps(t *testing.T) {
	vm := runVM(t, `
		package main

		var m = make("map[string]int")
		var v any
		func init() {
			m["one"] = 1
			v = m["one"]
			delete(m, "one")
		}
	`)
	checkInt(t, vm, "v", 1)
	mArg, _ := vm.Get("m")
	m := mArg.(map[any]any)
	if len(m) != 0 {
		t.Fatal("delete failed")
	}
}

func TestVMMapLiterals(t *testing.T) {
	vm := runVM(t, `
		package main
		var m = map[string]int{
			"a": 1,
			"b": 2,
		}
		var sum = m["a"] + m["b"]
	`)
	checkInt(t, vm, "sum", 3)
}

func TestVMRangeMap(t *testing.T) {
	vm := runVM(t, `
		package main
		var m = map[string]int{"a": 1, "b": 2}
		var sum = 0
		func init() {
			for k := range m {
				sum += m[k]
			}
		}
	`)
	checkInt(t, vm, "sum", 3)
}

func TestVMStringOps(t *testing.T) {
	vm := runVM(t, `
		package main
		var s = "hello" + " " + "world"
		var l = len(s)
		var sub = s[0:5]
	`)
	checkString(t, vm, "s", "hello world")
	checkInt(t, vm, "l", 11)
	checkString(t, vm, "sub", "hello")
}

func TestVMVariadicFunction(t *testing.T) {
	vm := runVM(t, `
		package main
		func sum(nums ...any) {
			var s = 0
			for n := range nums {
				s += n
			}
			return s
		}
		var res = sum(1, 2, 3, 4)
	`)
	checkInt(t, vm, "res", 10)
}

func TestVMIfElseJumpOffset(t *testing.T) {
	// This tests the off-by-one bug in patchJump.
	// If the jump lands at target+1, it skips 'x = 5' and executes what follows.
	vm := runVM(t, `
		package main
		var x = 0
		func init() {
			if false {
				x = 1
			} else {
				x = 5
			}
		}
	`)
	checkInt(t, vm, "x", 5)
}

func TestVMLogicOperators(t *testing.T) {
	vm := runVM(t, `
		package main
		var t1 = true && true
		var f1 = true && false
		var f2 = false && true
		var t2 = true || false
		var t3 = false || true
		var f3 = false || false
	`)
	checkBool(t, vm, "t1", true)
	checkBool(t, vm, "f1", false)
	checkBool(t, vm, "f2", false)
	checkBool(t, vm, "t2", true)
	checkBool(t, vm, "t3", true)
	checkBool(t, vm, "f3", false)
}

func TestVMLogicShortCircuit(t *testing.T) {
	vm := runVM(t, `
		package main
		var cnt = 0
		func inc() {
			cnt += 1
			return true
		}
		func init() {
			// Should not call inc
			var x = false && inc()
			// Should not call inc
			var y = true || inc()
		}
	`)
	checkInt(t, vm, "cnt", 0)
}

func TestVMExtendedOps(t *testing.T) {
	vm := runVM(t, `
		package main
		var resAndNot = 3 &^ 1 // 11 &^ 01 = 10 (2)
		var resLsh = 1 << 2    // 4
		var resRsh = 8 >> 1    // 4
		var resBitNot = ^1     // -2 (assuming 64-bit int logic in taivm)

		var inc = 0
		var dec = 10
		func init() {
			inc++
			dec--
		}
	`)
	checkInt(t, vm, "resAndNot", 2)
	checkInt(t, vm, "resLsh", 4)
	checkInt(t, vm, "resRsh", 4)
	checkInt(t, vm, "resBitNot", ^int64(1))
	checkInt(t, vm, "inc", 1)
	checkInt(t, vm, "dec", 9)
}

func TestVMLoops(t *testing.T) {
	vm := runVM(t, `
		package main
		
		var sum = 0
		func init() {
			for i := 0; i < 5; i++ {
				sum += i
			}
		}

		var breakVal = 0
		func init() {
			for i := 0; i < 10; i++ {
				breakVal = i
				if i == 5 {
					break
				}
			}
		}

		var continueSum = 0
		func init() {
			for i := 0; i < 5; i++ {
				if i == 2 {
					continue
				}
				continueSum += i
			}
		}

		var switchMulti = 0
		func init() {
			var x = 2
			switch x {
			case 1, 2, 3:
				switchMulti = 1
			default:
				switchMulti = 2
			}
		}
	`)
	checkInt(t, vm, "sum", 10)
	checkInt(t, vm, "breakVal", 5)
	// 0 + 1 + 3 + 4 = 8
	checkInt(t, vm, "continueSum", 8)
	checkInt(t, vm, "switchMulti", 1)
}

func TestVMCompositeAndSlice(t *testing.T) {
	vm := runVM(t, `
		package main

		var sLit = []int{1, 2, 3}
		
		var sMake = make("[]int", 3) // [0, 0, 0]
		
		var copied = 0
		func init() {
			// sMake is [0,0,0], sLit is [1,2,3]
			// copy(dst, src)
			copy(sMake, sLit)
			copied = sMake[1]
		}
		
		var sFull = sLit[:]
		var sHead = sLit[:2]
		var sTail = sLit[1:]
	`)

	val, _ := vm.Get("sLit")
	l := val.(*taivm.List)
	if len(l.Elements) != 3 {
		t.Fatalf("expected slice len 3, got %d", len(l.Elements))
	}

	checkInt(t, vm, "copied", 2)

	valFull, _ := vm.Get("sFull")
	if len(valFull.(*taivm.List).Elements) != 3 {
		t.Fatal("sFull length mismatch")
	}

	valHead, _ := vm.Get("sHead")
	if len(valHead.(*taivm.List).Elements) != 2 {
		t.Fatal("sHead length mismatch")
	}

	valTail, _ := vm.Get("sTail")
	if len(valTail.(*taivm.List).Elements) != 2 {
		t.Fatal("sTail length mismatch")
	}
}

func TestVMConversionsAndComplex(t *testing.T) {
	vm := runVM(t, `
		package main
		
		var c = complex(3, 4)
		var r = real(c)
		var i = imag(c)

		var iVal = int(1.9)
		var fVal = float64(10)
		var bVal1 = bool(1)
		var bVal2 = bool(0)
		var sVal = string(123)
		var sVal2 = string("hello")
	`)

	// We can't easily check complex directly with helpers, mostly ensuring it compiled and ran
	checkInt(t, vm, "iVal", 1) // int conversion truncates
	checkString(t, vm, "sVal", "123")
	checkString(t, vm, "sVal2", "hello")
	checkBool(t, vm, "bVal1", true)
	checkBool(t, vm, "bVal2", false)

	// Check float types manually
	if f, ok := vm.Get("fVal"); !ok || f.(float64) != 10.0 {
		t.Errorf("expected float 10.0, got %v", f)
	}
	if r, ok := vm.Get("r"); !ok || r.(float64) != 3.0 {
		t.Errorf("expected real 3.0, got %v", r)
	}
	if i, ok := vm.Get("i"); !ok || i.(float64) != 4.0 {
		t.Errorf("expected imag 4.0, got %v", i)
	}
}

func TestVMMiscFeatures(t *testing.T) {
	vm := runVM(t, `
		package main

		var ch = 'a'
		var ptr *int // *int just string in types, but usage expression:
		var x = 10
		var y = 0
		var z = 0

		var mainRun = false
		func main() {
			y = *x      // pointer deref
			z = x.(int) // type assertion
			mainRun = true
		}
	`)

	checkInt(t, vm, "ch", 'a')
	checkInt(t, vm, "y", 10)
	checkInt(t, vm, "z", 10)
	checkBool(t, vm, "mainRun", true)
}

func TestVMVariadicCallSpread(t *testing.T) {
	vm := runVM(t, `
		package main
		func sum(args ...any) {
			var t = 0
			for v := range args {
				t += v
			}
			return t
		}
		var s = []int{1, 2, 3}
		var res = sum(s...)
	`)
	checkInt(t, vm, "res", 6)
}

func TestVMCompileErrors(t *testing.T) {
	badSources := []string{
		"package main; func f() { var ch chan int; ch <- 1 }",
		"package main; func f() { var i any; switch i.(type) {} }",
		"package main; func f() { type T int }",
		"package main; func f() { fallthrough }",
		"package main; func f() { goto L; L: }",
	}

	for _, src := range badSources {
		_, err := NewVM("bad", strings.NewReader(src))
		if err == nil {
			t.Errorf("expected compilation error for source: %s", src)
		}
	}
}

func TestVMRuntimeImportErrors(t *testing.T) {
	vm, err := NewVM("main", strings.NewReader(`
		package main
		import "fmt"
	`))
	if err != nil {
		t.Fatal(err)
	}

	vm.Run(func(i *taivm.Interrupt, err error) bool {
		if err != nil {
			if strings.Contains(err.Error(), "import not implemented") {
				return false // Expected error
			}
			t.Fatalf("unexpected error: %v", err)
		}
		return true
	})
}

func TestVMNegativeCases(t *testing.T) {
	// Test Compilation Errors
	badSources := []string{
		"package main; func f() { go f() }",
		"package main; func f() { defer f() }",
		"package main; func f() { select {} }",
	}

	for _, src := range badSources {
		_, err := NewVM("bad", strings.NewReader(src))
		if err == nil {
			t.Errorf("expected compilation error for source: %s", src)
		}
	}

	// Test Runtime Errors (Panic, etc)
	vm := runVM(t, `
		package main
		func doPanic() {
			panic("oops")
		}
		func doBadMake() {
			make("chan int")
		}
	`)

	// Call Panic
	panicFuncVal, ok := vm.Get("doPanic")
	if !ok {
		t.Fatal("doPanic not found")
	}
	panicFunc := panicFuncVal.(*taivm.Closure)
	vm.CurrentFun = panicFunc.Fun
	vm.Scope = panicFunc.Env
	vm.IP = 0

	panicked := false
	vm.Run(func(i *taivm.Interrupt, err error) bool {
		if err != nil && strings.Contains(err.Error(), "oops") {
			panicked = true
			return false // stop vm
		}
		return true
	})
	if !panicked {
		t.Error("expected panic to yield error")
	}

	// Call Bad Make
	makeFuncVal, _ := vm.Get("doBadMake")
	makeFunc := makeFuncVal.(*taivm.Closure)
	vm.CurrentFun = makeFunc.Fun
	vm.Scope = makeFunc.Env
	vm.IP = 0

	makeFailed := false
	vm.Run(func(i *taivm.Interrupt, err error) bool {
		if err != nil && strings.Contains(err.Error(), "channels not supported") {
			makeFailed = true
			return false
		}
		return true
	})
	if !makeFailed {
		t.Error("expected make(chan) to fail")
	}
}

func TestVMCharVarInvalidSyntax(t *testing.T) {
	runVM(t, `
		package main
		var ch = 'a'
	`)
}
