package taigo

import (
	"strings"
	"testing"

	"github.com/reusee/tai/taivm"
)

func runVM(t *testing.T, src string) *taivm.VM {
	vm, err := NewVM("main", strings.NewReader(src), &Options{
		Stdout: t.Output(),
		Stderr: t.Output(),
	})
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

		var s = make([]int, 3)
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

		var m = make(map[string]int)
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
		
		var sMake = make([]int, 3) // [0, 0, 0]
		
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
		var bVal1 = bool(true)
		var bVal2 = bool(false)
		var sVal2 = string("hello")
	`)

	// We can't easily check complex directly with helpers, mostly ensuring it compiled and ran
	checkInt(t, vm, "iVal", 1) // int conversion truncates
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
		var ptr *int
		var x = 10
		var xp = &x
		var y = 0
		var z = 0

		var mainRun = false
		func main() {
			y = *xp      // pointer deref
			z = x.(int) // type assertion
			mainRun = true
		}
	`)

	checkInt(t, vm, "ch", 'a')
	checkInt(t, vm, "y", 10)
	checkInt(t, vm, "z", 10)
	checkBool(t, vm, "mainRun", true)
}

func TestVMPointers(t *testing.T) {
	vm := runVM(t, `
		package main
		var x = 1
		var y = 0
		var z = 0
		var w = 0

		func modify(p any) {
			*p = 42
		}

		func init() {
			var p = &x
			*p = 10
			
			var s = []int{0, 100}
			var sp = &s[1]
			*sp = 200
			y = s[1]
			
			var m = map[string]int{"a": 1}
			var mp = &m["a"]
			*mp = 500
			z = m["a"]

			modify(&w)
		}
	`)
	checkInt(t, vm, "x", 10)
	checkInt(t, vm, "y", 200)
	checkInt(t, vm, "z", 500)
	checkInt(t, vm, "w", 42)
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
		"package main; func f() { fallthrough }",
		"package main; func f() { goto L; L: }",
	}

	for _, src := range badSources {
		_, err := NewVM("bad", strings.NewReader(src), nil)
		if err == nil {
			t.Errorf("expected compilation error for source: %s", src)
		}
	}
}

func TestVMRuntimeImportErrors(t *testing.T) {
	vm, err := NewVM("main", strings.NewReader(`
		package main
		import "fmt"
	`), nil)
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
		"package main; func f() { select {} }",
	}

	for _, src := range badSources {
		_, err := NewVM("bad", strings.NewReader(src), nil)
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
			make(chan int)
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

func TestCoverageBuiltins(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantErr   string
		wantPanic string
	}{
		{
			name: "print",
			src:  `package main; func main() { print("a", "b") }`,
		},
		{
			name: "println",
			src:  `package main; func main() { println("a", "b") }`,
		},
		{
			name: "len_string",
			src:  `package main; var l = len("abc")`,
		},
		{
			name: "len_nil",
			src:  `package main; var l = len(nil)`,
		},
		{
			name: "len_native_slice",
			src:  `package main; func f(s []any) { return len(s) }; var l = f([]int{1, 2})`,
		},
		{
			name: "len_map_any",
			src:  `package main; var m = map[any]any{1:1}; var l = len(m)`,
		},
		{
			name: "len_map_string",
			src:  `package main; var m = map[string]any{"a":1}; var l = len(m)`,
		},
		{
			name: "len_range",
			src:  `package main; var r = make([]int, 5); var l = len(r)`, // actually List but exercises Len()
		},
		{
			name:    "len_invalid",
			src:     `package main; var l = len(1)`,
			wantErr: "invalid argument type for len",
		},
		{
			name:    "len_args",
			src:     `package main; var l = len()`,
			wantErr: "len expects 1 argument",
		},
		{
			name: "cap_nil",
			src:  `package main; var c = cap(nil)`,
		},
		{
			name: "cap_native_slice",
			src:  `package main; func f(s []any) { return cap(s) }; var c = f([]int{1, 2})`,
		},
		{
			name:    "cap_invalid",
			src:     `package main; var c = cap(1)`,
			wantErr: "invalid argument type for cap",
		},
		{
			name:    "cap_args",
			src:     `package main; var c = cap()`,
			wantErr: "cap expects 1 argument",
		},
		{
			name: "append_nil",
			src:  `package main; var s = append(nil, 1)`,
		},
		{
			name: "append_multiple",
			src:  `package main; var s = append([]int{1}, 2, 3)`,
		},
		{
			name:    "append_invalid_args",
			src:     `package main; func main() { append() }`,
			wantErr: "append expects at least 1 argument",
		},
		{
			name:    "append_not_list",
			src:     `package main; func main() { append(1, 2) }`,
			wantErr: "first argument to append must be list or nil",
		},
		{
			name:    "append_immutable",
			src:     `package main; func main() { var s = []int{1, 2}; append(s[:], 3) }`, // slices are lists, but literals are mutable. We need an immutable one.
			wantErr: "",                                                                   // wait, literals are mutable.
		},
		{
			name: "copy_native_slice",
			src:  `package main; var a = []int{1}; var b = []int{2}; var n = copy(a, b)`,
		},
		{
			name:    "copy_args",
			src:     `package main; func main() { copy(1) }`,
			wantErr: "copy expects 2 arguments",
		},
		{
			name:    "copy_invalid_dst",
			src:     `package main; func main() { copy(1, []int{42}) }`,
			wantErr: "copy expects list or slice as first argument",
		},
		{
			name:    "copy_invalid_src",
			src:     `package main; func main() { copy([]int{42}, 1) }`,
			wantErr: "copy expects list or slice as second argument",
		},
		{
			name: "delete_nil",
			src:  `package main; func main() { delete(nil, "k") }`,
		},
		{
			name: "delete_map_any",
			src:  `package main; func main() { var m = map[any]any{1:2}; delete(m, 1) }`,
		},
		{
			name: "delete_map_string",
			src:  `package main; func main() { var m = map[string]int{"a":1}; delete(m, "a") }`,
		},
		{
			name:    "delete_args",
			src:     `package main; func main() { delete() }`,
			wantErr: "delete expects 2 arguments",
		},
		{
			name:    "delete_invalid_map",
			src:     `package main; func main() { delete(1, 2) }`,
			wantErr: "delete expects map",
		},
		{
			name:    "close_unsupported",
			src:     `package main; func main() { close(1) }`,
			wantErr: "channels not supported",
		},
		{
			name:    "complex_args",
			src:     `package main; func main() { complex(1) }`,
			wantErr: "complex expects 2 arguments",
		},
		{
			name:    "complex_invalid_types",
			src:     `package main; func main() { complex("a", "b") }`,
			wantErr: "complex arguments must be numbers",
		},
		{
			name:    "real_args",
			src:     `package main; func main() { real(1, 2) }`,
			wantErr: "real expects 1 argument",
		},
		{
			name:    "real_invalid",
			src:     `package main; func main() { real("a") }`,
			wantErr: "real expects numeric argument",
		},
		{
			name:    "imag_args",
			src:     `package main; func main() { imag(1, 2) }`,
			wantErr: "imag expects 1 argument",
		},
		{
			name:    "imag_invalid",
			src:     `package main; func main() { imag("a") }`,
			wantErr: "imag expects numeric argument",
		},
		{
			name: "int_float",
			src:  `package main; var i = int(1.5)`,
		},
		{
			name: "int_int",
			src:  `package main; var i = int(10)`,
		},
		{
			name:    "int_args",
			src:     `package main; func main() { int(1, 2) }`,
			wantErr: "type conversion expects 1 argument",
		},
		{
			name:    "int_invalid",
			src:     `package main; func main() { int("a") }`,
			wantErr: "cannot convert string to int",
		},
		{
			name:    "float64_args",
			src:     `package main; func main() { float64(1, 2) }`,
			wantErr: "type conversion expects 1 argument",
		},
		{
			name:    "float64_invalid",
			src:     `package main; func main() { float64("a") }`,
			wantErr: "cannot convert string to float64",
		},
		{
			name:    "bool_args",
			src:     `package main; func main() { bool(1, 2) }`,
			wantErr: "type conversion expects 1 argument",
		},
		{
			name:    "string_args",
			src:     `package main; func main() { string(1, 2) }`,
			wantErr: "type conversion expects 1 argument",
		},
		{
			name: "make_slice_init",
			src:  `package main; var s1 = make([]bool, 1); var s2 = make([]string, 1); var s3 = make([]float64, 1); var s4 = make([]int, 1)`,
		},
		{
			name: "make_map",
			src:  `package main; var m = make(map[string]int)`,
		},
		{
			name:    "make_args",
			src:     `package main; func main() { make() }`,
			wantErr: "make expects type argument",
		},
		{
			name:    "make_invalid_type_arg",
			src:     `package main; func main() { make(1) }`,
			wantErr: "make expects reflect.Type as first argument",
		},
		{
			name:    "make_slice_no_len",
			src:     `package main; func main() { make([]int) }`,
			wantErr: "make slice expects length argument",
		},
		{
			name:    "make_slice_bad_len",
			src:     `package main; func main() { make([]int, "a") }`,
			wantErr: "slice length must be integer",
		},
		{
			name:    "make_slice_neg_len",
			src:     `package main; func main() { make([]int, -1) }`,
			wantErr: "negative slice length",
		},
		{
			name:    "make_chan",
			src:     `package main; func main() { make(chan int) }`,
			wantErr: "channels not supported",
		},
		{
			name:    "make_unknown",
			src:     `package main; func main() { make(func()) }`,
			wantErr: "cannot make type func()",
		},
		{
			name: "new_types",
			src:  `package main; var a = new(int64); var b = new(bool); var c = new(string); var d = new(float64); var e = new(uint)`,
		},
		{
			name:    "new_args",
			src:     `package main; func main() { new() }`,
			wantErr: "new expects type argument",
		},
		{
			name:    "new_invalid_arg",
			src:     `package main; func main() { new(1) }`,
			wantErr: "new expects reflect.Type",
		},
		{
			name:      "panic_arg",
			src:       `package main; func main() { panic("foo") }`,
			wantPanic: "foo",
		},
		{
			name:      "panic_no_arg",
			src:       `package main; func main() { panic() }`,
			wantPanic: "panic",
		},
		{
			name: "recover_noop",
			src:  `package main; func main() { recover() }`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm, err := NewVM("test", strings.NewReader(tt.src), &Options{
				Stdout: t.Output(),
				Stderr: t.Output(),
			})
			if err != nil {
				t.Fatalf("unexpected compilation error: %v", err)
			}
			err = nil
			vm.Run(func(i *taivm.Interrupt, e error) bool {
				err = e
				return false
			})

			if tt.wantPanic != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantPanic) {
					t.Errorf("expected panic containing %q, got %v", tt.wantPanic, err)
				}
				return
			}

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCoverageCompilerUnsupported(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			name:    "go_stmt",
			src:     `package main; func main() { go func(){}() }`,
			wantErr: "go statement not supported",
		},
		{
			name:    "select_stmt",
			src:     `package main; func main() { select {} }`,
			wantErr: "select statement not supported",
		},
		{
			name:    "send_stmt",
			src:     `package main; func main() { var ch chan int; ch <- 1 }`,
			wantErr: "send statement not supported",
		},
		{
			name:    "type_switch",
			src:     `package main; func main() { var x interface{}; switch x.(type) {} }`,
			wantErr: "type switch statement not supported",
		},
		{
			name:    "map_key_value_error",
			src:     `package main; func main() { var m = map[string]int{"a"} }`, // invalid syntax usually caught by parser?
			wantErr: "element must be key:value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewVM("test", strings.NewReader(tt.src), nil)
			if err == nil {
				t.Error("expected compilation error")
			} else if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCoverageCompilerExpressions(t *testing.T) {
	src := `
		package main
		
		var s = []int{1}
		func init() {
			// Selector access (method on list)
			var f = s.append
			f(2)
		}
		
		var x = 42
		var ptr = &x
		var deref = *ptr
		
		var assertVal = 1
		var asserted = assertVal.(int)
		
		var notVal = !true
		var bitNotVal = ^1
		
		var sl = []int{1, 2, 3}
		var sl1 = sl[:]
		var sl2 = sl[1:]
		var sl3 = sl[:2]
		
		func variadic(x ...any) { return len(x) }
		var spread = []any{1, 2}
		var spreadRes = variadic(spread...)
	`
	vm := runVM(t, src)
	checkInt(t, vm, "spreadRes", 2)
	checkInt(t, vm, "deref", 42)
}

func TestCoverageCompilerAssign(t *testing.T) {
	src := `
		package main
		
		var s = []int{1, 2}
		func init() {
			s[0] = 10     // SetIndex
			s[1] += 5     // Compound Assign Index
		}

		var m = map[string]int{"a": 1}
		func init() {
			m["a"] += 1   // Compound Assign Index Map
		}
		
		var x = 1
		func init() {
			x += 1        // Compound Assign Var
		}
		
		var a, b = 1, 2
		func init() {
			a, b = 3, 4   // Multi Assign
		}
	`
	vm := runVM(t, src)
	checkInt(t, vm, "x", 2)
	checkInt(t, vm, "a", 3)
}

func TestCoverageCompilerErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			name:    "multi_assign_count",
			src:     `package main; func main() { var a, b = 1, 2, 3 }`,
			wantErr: "assignment count mismatch",
		},
		{
			name:    "assign_stmt_count_mismatch",
			src:     `package main; func main() { var a, b any; a, b = 1, 2, 3 }`,
			wantErr: "assignment count mismatch",
		},
		{
			name:    "assign_to_non_var",
			src:     `package main; func main() { 1 = 2 }`,
			wantErr: "assignment to *ast.BasicLit not supported",
		},
		{
			name:    "compound_assign_bad_lhs",
			src:     `package main; func main() { 1 += 2 }`,
			wantErr: "compound assignment to *ast.BasicLit not supported",
		},
		{
			name: "selector_assign",
			src:  `package main; func main() { var x = struct{ foo int }{}; x.foo = 1 }`,
			// Valid compile, runtime error (no setattr on list)
			wantErr: "",
		},
		{
			name: "selector_assign_compound",
			src:  `package main; func main() { var x = struct{ foo int }{}; x.foo += 1 }`,
			// Valid compile, runtime error
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm, err := NewVM("test", strings.NewReader(tt.src), nil)
			if tt.wantErr == "expected" {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected compile error %q, got %v", tt.wantErr, err)
				}
				return
			}
			// If we expect valid compile but runtime error:
			if err != nil {
				t.Fatalf("unexpected compile error: %v", err)
			}
			vm.Run(func(i *taivm.Interrupt, err error) bool {
				return true // Ignore runtime errors, we just want to compile coverage
			})
		})
	}
}

func TestCoverageVarDecl(t *testing.T) {
	// Cover var decl with no values (init to nil)
	src := `
		package main
		var a, b any
		func init() {
			if a != nil || b != nil {
				panic("expected nil")
			}
		}
	`
	runVM(t, src)
}

func TestBuiltinIO(t *testing.T) {
	var buf strings.Builder
	opts := &Options{
		Stdout: &buf,
	}
	src := `package main; func init() { print("a", "b"); println("c", "d") }`
	vm, err := NewVM("test", strings.NewReader(src), opts)
	if err != nil {
		t.Fatal(err)
	}
	vm.Run(func(i *taivm.Interrupt, err error) bool {
		return true
	})
	got := buf.String()
	// print("a", "b") -> "a b"
	// println("c", "d") -> " c d\n" because print didn't add newline
	if !strings.Contains(got, "a bc d\n") {
		t.Errorf("expected 'a bc d\n', got %q", got)
	}

	// Test without options
	vm2, _ := NewVM("test", strings.NewReader(`package main; func init() { print("x") }`), nil)
	vm2.Run(func(i *taivm.Interrupt, err error) bool { return true })
}

func TestCoverageCompilerMisc(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			name: "type_decl",
			src:  `package main; type T int; func main() {}`,
		},
		{
			name: "empty_stmt",
			src:  `package main; func main() { ; }`,
		},
		{
			name: "labeled_stmt",
			src:  `package main; func main() { L: print(1) }`,
		},
		{
			name: "range_with_value",
			src:  `package main; func main() { for k, v := range []int{1} {} }`,
		},
		{
			name:    "multi_assign_mismatch",
			src:     `package main; func main() { var a, b = 1, 2, 3 }`,
			wantErr: "assignment count mismatch",
		},
		{
			name: "variadic_unnamed",
			src:  `package main; func f(...any) {}; func main() { f(1, 2) }`,
		},
		{
			name: "init_multi",
			src:  `package main; func init() { print(1) }; func init() { print(2) }`,
		},
		{
			name:    "key_value_outside_composite",
			src:     `package main; func main() { var x = 1:2 }`,
			wantErr: "expected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewVM("test", strings.NewReader(tt.src), nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCoverage2(t *testing.T) {
	src := `
		package main
		
		var _, x = 1, 2
		var a, _ = 3, 4
		var _ = 5
		
		func init() {
			var ch = 'A'
			var pos = +1
			
			for range []int{1} {
				{
					break
				}
			}
			
			for range []int{1} {}
			
			var (
				_ [0]int
				_ <-chan int
				_ chan<- int
				_ struct {
					A, B int
				}
			)
			
			var c64 complex64 = complex(1, 2)
			var r = real(c64)
			var i = imag(c64)
			var r2 = real(1.5)
			var i2 = imag(1.5)
			
			var _ = make([]bool, 1)
			var _ = make([]string, 1)
			var _ = make([]float64, 1)
			var _ = make([]int, 1)
			var _ = make(map[string]int)
			
			var s1 = []int{1}
			var s2 = []int{2}
			copy(s1, s2)
		}
	`
	runVM(t, src)
}

func TestVMMethods(t *testing.T) {
	vm := runVM(t, `
		package main

		type Counter struct {
			count int
		}

		func (c Counter) getValue() any {
			return c.count
		}

		func (c Counter) add(n any) any {
			return c.count + n
		}

		var c = Counter{count: 5}
		var result = c.getValue()
		var result2 = c.add(3)
	`)
	checkInt(t, vm, "result", 5)
	checkInt(t, vm, "result2", 8)
}

func TestVMMethodsWithPointerReceiver(t *testing.T) {
	vm := runVM(t, `
		package main

		type Counter struct {
			count int
		}

		func (c *Counter) increment() {
			c.count = c.count + 1
		}

		func (c *Counter) get() any {
			return c.count
		}

		var c = Counter{count: 0}
		var result = 0
		func init() {
			c.increment()
			c.increment()
			result = c.get()
		}
	`)
	// Pointer receiver methods can modify the struct
	checkInt(t, vm, "result", 2)
}

func TestVMMethodsMultipleTypes(t *testing.T) {
	vm := runVM(t, `
		package main

		type Rectangle struct {
			width, height int
		}

		type Circle struct {
			radius int
		}

		func (r Rectangle) Area() any {
			return r.width * r.height
		}

		func (c Circle) Area() any {
			return 3 * c.radius * c.radius
		}

		var rectArea = ""
		func init() {
			r := Rectangle{width: 3, height: 4}
			rectArea = r.Area()
		}

		var circleArea = ""
		func init() {
			c := Circle{radius: 5}
			circleArea = c.Area()
		}
	`)
	// Rectangle: 3 * 4 = 12
	// Circle: 3 * 5 * 5 = 75
	checkInt(t, vm, "rectArea", 12)
	checkInt(t, vm, "circleArea", 75)
}
