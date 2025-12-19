package taipy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/reusee/tai/taivm"
)

func run(t *testing.T, src string) *taivm.VM {
	vm, err := NewVM("test", strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}

	for _, err := range vm.Run {
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
	}

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

	for _, err := range vm.Run {
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
	}

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

	for _, err := range vm.Run {
		if err != nil {
			errOccurred = true
		}
	}

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
	hasErr := false

	for _, err := range vm.Run {
		if err != nil {
			hasErr = true
			if !strings.Contains(err.Error(), "missing argument") {
				t.Errorf("unexpected error: %v", err)
			}
		}
	}

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
	hasErr = false

	for _, err := range vm.Run {
		if err != nil {
			hasErr = true
			if !strings.Contains(err.Error(), "unexpected keyword argument") {
				t.Errorf("unexpected error: %v", err)
			}
		}
	}

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
	keys += [k]
`)
	if val, ok := vm.Get("keys"); !ok {
		t.Errorf("keys not found")
	} else {
		sl := val.(*taivm.List).Elements
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
		sl := val.(*taivm.List).Elements
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
		sl, ok := val.(*taivm.List)
		if !ok {
			t.Errorf("expected list, got %T", val)
		} else if !sl.Immutable {
			t.Error("expected immutable list")
		} else {
			if len(sl.Elements) != 3 {
				t.Errorf("len(res) = %d, want 3", len(sl.Elements))
			} else if sl.Elements[0] != int64(2) || sl.Elements[2] != int64(4) {
				t.Errorf("res = %v", sl.Elements)
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
		sl := val.(*taivm.List).Elements
		if len(sl) != 3 {
			t.Errorf("len(res) = %d, want 3", len(sl))
		} else if sl[0] != int64(1) || sl[1] != int64(3) || sl[2] != int64(5) {
			t.Errorf("res = %v", sl)
		}
	}
}

func TestCompileAugmentedAssign(t *testing.T) {
	// Simple variable
	vm := run(t, `
x = 1
x += 2
y = 10
y -= 3
z = 2
z *= 4
`)
	if val, ok := vm.Get("x"); !ok || val != int64(3) {
		t.Errorf("x = %v, want 3", val)
	}
	if val, ok := vm.Get("y"); !ok || val != int64(7) {
		t.Errorf("y = %v, want 7", val)
	}
	if val, ok := vm.Get("z"); !ok || val != int64(8) {
		t.Errorf("z = %v, want 8", val)
	}

	// List index
	vm = run(t, `
l = [10, 20]
l[0] += 5
l[1] -= 5
`)
	if val, ok := vm.Get("l"); !ok {
		t.Error("l not found")
	} else {
		sl := val.(*taivm.List).Elements
		if sl[0] != int64(15) {
			t.Errorf("l[0] = %v, want 15", sl[0])
		}
		if sl[1] != int64(15) {
			t.Errorf("l[1] = %v, want 15", sl[1])
		}
	}

	// Map index
	vm = run(t, `
d = {"a": 100}
d["a"] += 50
`)
	if val, ok := vm.Get("d"); !ok {
		t.Error("d not found")
	} else {
		m := val.(map[any]any)
		if m["a"] != int64(150) {
			t.Errorf("d['a'] = %v, want 150", m["a"])
		}
	}
}

func TestCompileDotExpr(t *testing.T) {
	s := &taivm.Struct{
		Fields: map[string]any{
			"x": int64(10),
		},
	}

	src := `
res = s.x
s.y = 20
s.x = 30
`
	fn, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	vm := taivm.NewVM(fn)
	vm.Def("s", s)

	for _, err := range vm.Run {
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
	}

	if val, ok := vm.Get("res"); !ok || val != int64(10) {
		t.Errorf("res = %v, want 10", val)
	}
	if val, ok := s.Fields["y"]; !ok || val != int64(20) {
		t.Errorf("s.y = %v, want 20", val)
	}
	if val, ok := s.Fields["x"]; !ok || val != int64(30) {
		t.Errorf("s.x = %v, want 30", val)
	}

	// Augmented assignment
	src = `
s.x += 5
`
	fn, err = Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	vm = taivm.NewVM(fn)
	vm.Def("s", s)

	for _, err := range vm.Run {
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
	}

	if val, ok := s.Fields["x"]; !ok || val != int64(35) {
		t.Errorf("s.x (augmented) = %v, want 35", val)
	}
}

func TestCompileCondExpr(t *testing.T) {
	vm := run(t, `
res1 = 1 if 1 < 2 else 0
res2 = 1 if 1 > 2 else 0
`)
	if val, ok := vm.Get("res1"); !ok || val != int64(1) {
		t.Errorf("res1 = %v, want 1", val)
	}
	if val, ok := vm.Get("res2"); !ok || val != int64(0) {
		t.Errorf("res2 = %v, want 0", val)
	}
}

func TestCompileLambda(t *testing.T) {
	vm := run(t, `
inc = lambda x: x + 1
res = inc(10)

add = lambda a, b: a + b
res2 = add(5, 7)
`)
	if val, ok := vm.Get("res"); !ok || val != int64(11) {
		t.Errorf("res = %v, want 11", val)
	}
	if val, ok := vm.Get("res2"); !ok || val != int64(12) {
		t.Errorf("res2 = %v, want 12", val)
	}
}

func TestCompileVariadic(t *testing.T) {
	vm := run(t, `
def f(a, *b):
	return b

l1 = f(1)
l2 = f(1, 2)
l3 = f(1, 2, 3)
`)
	if val, ok := vm.Get("l1"); !ok {
		t.Error("l1 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 0 {
		t.Errorf("l1 = %v, want []", val)
	}

	if val, ok := vm.Get("l2"); !ok {
		t.Error("l2 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 1 || l.Elements[0].(int64) != 2 {
		t.Errorf("l2 = %v, want [2]", val)
	}

	if val, ok := vm.Get("l3"); !ok {
		t.Error("l3 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 2 || l.Elements[1].(int64) != 3 {
		t.Errorf("l3 = %v, want [2, 3]", val)
	}
}

func TestCompileVariadicLambda(t *testing.T) {
	vm := run(t, `
f = lambda a, *b: b
l1 = f(1)
l2 = f(1, 2, 3)
`)
	if val, ok := vm.Get("l1"); !ok {
		t.Error("l1 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 0 {
		t.Errorf("l1 = %v, want []", val)
	}

	if val, ok := vm.Get("l2"); !ok {
		t.Error("l2 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 2 || l.Elements[0].(int64) != 2 {
		t.Errorf("l2 = %v, want [2, 3]", val)
	}
}

func TestCompileCallUnpack(t *testing.T) {
	// Unpack list
	vm := run(t, `
def f(a, b):
	return a + b
args = [10, 20]
res = f(*args)
`)
	if val, ok := vm.Get("res"); !ok || val != int64(30) {
		t.Errorf("res = %v, want 30", val)
	}

	// Unpack map
	vm = run(t, `
def g(x, y):
	return x * y
kwargs = {"x": 3, "y": 4}
res2 = g(**kwargs)
`)
	if val, ok := vm.Get("res2"); !ok || val != int64(12) {
		t.Errorf("res2 = %v, want 12", val)
	}

	// Mixed
	vm = run(t, `
def h(a, b, c, d):
	return a + b + c + d
res3 = h(1, *[2], c=3, **{"d": 4})
`)
	if val, ok := vm.Get("res3"); !ok || val != int64(10) {
		t.Errorf("res3 = %v, want 10", val)
	}
}

func TestNativeLen(t *testing.T) {
	vm := run(t, `
l = [1, 2, 3]
len_l = len(l)
len_s = len("hello")
len_u = len("你好")
d = {"a": 1}
len_d = len(d)
t = (1, 2)
len_t = len(t)
`)
	if val, ok := vm.Get("len_l"); !ok || val != int64(3) {
		t.Errorf("len_l = %v, want 3", val)
	}
	if val, ok := vm.Get("len_s"); !ok || val != int64(5) {
		t.Errorf("len_s = %v, want 5", val)
	}
	if val, ok := vm.Get("len_u"); !ok || val != int64(2) {
		t.Errorf("len_u = %v, want 2", val)
	}
	if val, ok := vm.Get("len_d"); !ok || val != int64(1) {
		t.Errorf("len_d = %v, want 1", val)
	}
	if val, ok := vm.Get("len_t"); !ok || val != int64(2) {
		t.Errorf("len_t = %v, want 2", val)
	}
}

func TestNativeRange(t *testing.T) {
	vm := run(t, `
sum = 0
for i in range(5):
	sum += i
`)
	if val, ok := vm.Get("sum"); !ok || val != int64(10) {
		t.Errorf("sum(range(5)) = %v, want 10", val)
	}

	vm = run(t, `
sum = 0
for i in range(1, 5):
	sum += i
`)
	// 1+2+3+4 = 10
	if val, ok := vm.Get("sum"); !ok || val != int64(10) {
		t.Errorf("sum(range(1, 5)) = %v, want 10", val)
	}

	vm = run(t, `
sum = 0
for i in range(0, 10, 2):
	sum += i
`)
	// 0+2+4+6+8 = 20
	if val, ok := vm.Get("sum"); !ok || val != int64(20) {
		t.Errorf("sum(range(0, 10, 2)) = %v, want 20", val)
	}
}

func TestNativePrint(t *testing.T) {
	// Just ensure it doesn't crash, as capturing stdout is harder
	run(t, `
print("hello", "world")
print(1, 2, 3)
`)
}

func TestNativeRangeSequence(t *testing.T) {
	// Len
	vm := run(t, `
l1 = len(range(10))
l2 = len(range(1, 11))
l3 = len(range(0, 10, 2))
l4 = len(range(10, 0, -1))
l5 = len(range(10, 0, -2))
l_empty = len(range(10, 0, 1))
`)
	if val, ok := vm.Get("l1"); !ok || val != int64(10) {
		t.Errorf("l1 = %v, want 10", val)
	}
	if val, ok := vm.Get("l2"); !ok || val != int64(10) {
		t.Errorf("l2 = %v, want 10", val)
	}
	if val, ok := vm.Get("l3"); !ok || val != int64(5) {
		t.Errorf("l3 = %v, want 5", val)
	}
	if val, ok := vm.Get("l4"); !ok || val != int64(10) {
		t.Errorf("l4 = %v, want 10", val)
	}
	if val, ok := vm.Get("l5"); !ok || val != int64(5) {
		t.Errorf("l5 = %v, want 5", val)
	}
	if val, ok := vm.Get("l_empty"); !ok || val != int64(0) {
		t.Errorf("l_empty = %v, want 0", val)
	}

	// Index
	vm = run(t, `
r = range(10, 20, 2)
v0 = r[0]
v1 = r[1]
v_last = r[-1]
`)
	if val, ok := vm.Get("v0"); !ok || val != int64(10) {
		t.Errorf("v0 = %v, want 10", val)
	}
	if val, ok := vm.Get("v1"); !ok || val != int64(12) {
		t.Errorf("v1 = %v, want 12", val)
	}
	if val, ok := vm.Get("v_last"); !ok || val != int64(18) {
		t.Errorf("v_last = %v, want 18", val)
	}
}
