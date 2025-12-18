package taipy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/reusee/tai/taivm"
)

func run(t *testing.T, src string) *taivm.VM {
	fn, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	vm := taivm.NewVM(fn)
	vm.Def("__apply_kw", ApplyKw)
	vm.Def("concat", Concat)

	vm.Run(func(intr *taivm.Interrupt, err error) bool {
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		return false
	})
	return vm
}

func TestCompileBasic(t *testing.T) {
	vm := run(t, `
a = 1 + 2
b = a * 3
`)
	if val, ok := vm.Get("a"); !ok || val != int64(3) {
		t.Errorf("a = %v, want 3", val)
	}
	if val, ok := vm.Get("b"); !ok || val != int64(9) {
		t.Errorf("b = %v, want 9", val)
	}
}

func TestCompileIf(t *testing.T) {
	vm := run(t, `
res = 0
if 1 < 2:
	res = 1
else:
	res = 2
`)
	if val, ok := vm.Get("res"); !ok || val != int64(1) {
		t.Errorf("res = %v %T, want 1", val, val)
	}

	vm = run(t, `
res = 0
if 1 > 2:
	res = 1
else:
	res = 2
`)
	if val, ok := vm.Get("res"); !ok || val != int64(2) {
		t.Errorf("res = %v, want 2", val)
	}
}

func TestCompileWhile(t *testing.T) {
	vm := run(t, `
sum = 0
i = 0
while i < 5:
	sum = sum + i
	i = i + 1
`)
	if val, ok := vm.Get("sum"); !ok || val != int64(10) {
		t.Errorf("sum = %v, want 10", val)
	}
}

func TestCompileFunction(t *testing.T) {
	vm := run(t, `
def add(a, b):
	return a + b
res = add(3, 4)
`)
	if val, ok := vm.Get("res"); !ok || val != int64(7) {
		t.Errorf("res = %v, want 7", val)
	}
}

func TestCompileRecursion(t *testing.T) {
	vm := run(t, `
def fib(n):
	if n <= 1:
		return n
	return fib(n-1) + fib(n-2)
res = fib(10)
`)
	// fib(10) = 55
	if val, ok := vm.Get("res"); !ok || val != int64(55) {
		t.Errorf("res = %v, want 55", val)
	}
}

func TestCompileList(t *testing.T) {
	vm := run(t, `
l = [1, 2, 3]
res = l[1]
l[2] = 5
res2 = l[2]
`)
	if val, ok := vm.Get("res"); !ok || val != int64(2) {
		t.Errorf("res = %v, want 2", val)
	}
	if val, ok := vm.Get("res2"); !ok || val != int64(5) {
		t.Errorf("res2 = %v, want 5", val)
	}
}

func TestCompileMap(t *testing.T) {
	vm := run(t, `
d = {"a": 1, "b": 2}
res = d["a"]
d["c"] = 3
res2 = d["c"]
`)
	if val, ok := vm.Get("res"); !ok || val != int64(1) {
		t.Errorf("res = %v, want 1", val)
	}
	if val, ok := vm.Get("res2"); !ok || val != int64(3) {
		t.Errorf("res2 = %v, want 3", val)
	}
}

func TestCompileControlFlow(t *testing.T) {
	vm := run(t, `
sum = 0
i = 0
while i < 10:
	i = i + 1
	if i % 2 == 0:
		continue
	if i > 5:
		break
	sum = sum + i
`)
	// 1 + 3 + 5 = 9
	if val, ok := vm.Get("sum"); !ok || val != int64(9) {
		t.Errorf("sum = %v, want 9", val)
	}
}

func TestCompileClosure(t *testing.T) {
	vm := run(t, `
def make_adder(x):
	def adder(y):
		return x + y
	return adder

add5 = make_adder(5)
res = add5(3)
`)
	if val, ok := vm.Get("res"); !ok || val != int64(8) {
		t.Errorf("res = %v, want 8", val)
	}
}

func TestNativeFunc(t *testing.T) {
	src := `
res = native_add(10, 20)
`
	fn, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	vm := taivm.NewVM(fn)
	vm.Def("native_add", taivm.NativeFunc{
		Name: "native_add",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("want 2 args")
			}
			return args[0].(int64) + args[1].(int64), nil
		},
	})

	vm.Run(func(intr *taivm.Interrupt, err error) bool {
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		return false
	})

	if val, ok := vm.Get("res"); !ok || val != int64(30) {
		t.Errorf("res = %v, want 30", val)
	}
}

func TestCompileError(t *testing.T) {
	_, err := Compile("test", strings.NewReader("if"))
	if err == nil {
		t.Fatal("expected compile error")
	}
}

func TestRuntimeError(t *testing.T) {
	fn, err := Compile("test", strings.NewReader("a = b"))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	vm := taivm.NewVM(fn)
	errOccurred := false
	vm.Run(func(intr *taivm.Interrupt, err error) bool {
		if err != nil {
			errOccurred = true
		}
		return false
	})
	if !errOccurred {
		t.Fatal("expected runtime error")
	}
}

func TestCompileKeywordArgs(t *testing.T) {
	vm := run(t, `
def sub(a, b):
	return a - b

res1 = sub(10, 3)
res2 = sub(a=10, b=3)
res3 = sub(b=3, a=10)
res4 = sub(10, b=3)
`)
	if val, ok := vm.Get("res1"); !ok || val != int64(7) {
		t.Errorf("res1 = %v, want 7", val)
	}
	if val, ok := vm.Get("res2"); !ok || val != int64(7) {
		t.Errorf("res2 = %v, want 7", val)
	}
	if val, ok := vm.Get("res3"); !ok || val != int64(7) {
		t.Errorf("res3 = %v, want 7", val)
	}
	if val, ok := vm.Get("res4"); !ok || val != int64(7) {
		t.Errorf("res4 = %v, want 7", val)
	}
}

func TestCompileKeywordArgsError(t *testing.T) {
	// Missing argument
	src := `
def f(a, b): return a+b
f(a=1)
`
	fn, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	vm := taivm.NewVM(fn)
	vm.Def("__apply_kw", ApplyKw)
	hasErr := false
	vm.Run(func(intr *taivm.Interrupt, err error) bool {
		if err != nil {
			hasErr = true
			if !strings.Contains(err.Error(), "missing argument") {
				t.Errorf("unexpected error: %v", err)
			}
		}
		return false
	})
	if !hasErr {
		t.Error("expected runtime error")
	}

	// Unexpected argument
	src = `
def f(a): return a
f(b=1)
`
	fn, _ = Compile("test", strings.NewReader(src))
	vm = taivm.NewVM(fn)
	vm.Def("__apply_kw", ApplyKw)
	hasErr = false
	vm.Run(func(intr *taivm.Interrupt, err error) bool {
		if err != nil {
			hasErr = true
			if !strings.Contains(err.Error(), "unexpected keyword argument") {
				t.Errorf("unexpected error: %v", err)
			}
		}
		return false
	})
	if !hasErr {
		t.Error("expected runtime error")
	}
}

func TestCompileFor(t *testing.T) {
	// List iteration
	vm := run(t, `
sum = 0
for i in [1, 2, 3]:
	sum = sum + i
`)
	if val, ok := vm.Get("sum"); !ok || val != int64(6) {
		t.Errorf("sum = %v, want 6", val)
	}

	// Map iteration
	vm = run(t, `
d = {"a": 1, "b": 2}
keys = []
for k in d:
	keys = concat(keys, [k])
`)
	if val, ok := vm.Get("keys"); !ok {
		t.Errorf("keys not found")
	} else {
		sl := val.([]any)
		if len(sl) != 2 {
			t.Errorf("keys len = %d, want 2", len(sl))
		}
		// Since we sort keys in OpGetIter, we expect ["a", "b"]
		if sl[0] != "a" || sl[1] != "b" {
			t.Errorf("keys = %v, want ['a', 'b']", sl)
		}
	}

	// Break and Continue
	vm = run(t, `
sum = 0
for i in [1, 2, 3, 4, 5]:
	if i == 2:
		continue
	if i == 4:
		break
	sum = sum + i
`)
	// 1 + 3 = 4 (skip 2, break at 4)
	if val, ok := vm.Get("sum"); !ok || val != int64(4) {
		t.Errorf("sum = %v, want 4", val)
	}
}

func TestCompileTuple(t *testing.T) {
	vm := run(t, `
t = (1, 2, 3)
res = t[1]
`)
	if val, ok := vm.Get("res"); !ok || val != int64(2) {
		t.Errorf("res = %v, want 2", val)
	}

	vm = run(t, `
t = (1,)
res = t[0]
`)
	if val, ok := vm.Get("res"); !ok || val != int64(1) {
		t.Errorf("res = %v, want 1", val)
	}
}

func TestCompileSlice(t *testing.T) {
	// List slice
	vm := run(t, `
l = [1, 2, 3, 4, 5]
res = l[1:4]
`)
	if val, ok := vm.Get("res"); !ok {
		t.Errorf("res not found")
	} else {
		sl := val.([]any)
		if len(sl) != 3 {
			t.Errorf("len(res) = %d, want 3", len(sl))
		} else if sl[0] != int64(2) || sl[2] != int64(4) {
			t.Errorf("res = %v", sl)
		}
	}

	// Tuple slice
	vm = run(t, `
t = (1, 2, 3, 4, 5)
res = t[1:4]
`)
	if val, ok := vm.Get("res"); !ok {
		t.Errorf("res not found")
	} else {
		sl, ok := val.(taivm.Tuple)
		if !ok {
			t.Errorf("expected tuple, got %T", val)
		} else {
			if len(sl) != 3 {
				t.Errorf("len(res) = %d, want 3", len(sl))
			} else if sl[0] != int64(2) || sl[2] != int64(4) {
				t.Errorf("res = %v", sl)
			}
		}
	}

	// String slice
	vm = run(t, `
s = "hello"
res = s[1:4]
`)
	if val, ok := vm.Get("res"); !ok || val != "ell" {
		t.Errorf("res = %v, want 'ell'", val)
	}

	// Step
	vm = run(t, `
l = [1, 2, 3, 4, 5]
res = l[::2]
`)
	if val, ok := vm.Get("res"); !ok {
		t.Errorf("res not found")
	} else {
		sl := val.([]any)
		if len(sl) != 3 {
			t.Errorf("len(res) = %d, want 3", len(sl))
		} else if sl[0] != int64(1) || sl[1] != int64(3) || sl[2] != int64(5) {
			t.Errorf("res = %v", sl)
		}
	}
}
