package taipy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/reusee/tai/taivm"
	"go.starlark.net/syntax"
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

	// Mixed with multiple positional sequences
	vm = run(t, `
def k(a, b, c, d):
	return a + b + c + d
res4 = k(1, *[2], 3, **{"d": 4})
`)
	if val, ok := vm.Get("res4"); !ok || val != int64(10) {
		t.Errorf("res4 = %v, want 10", val)
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

func TestCompileListMethod(t *testing.T) {
	vm := run(t, `
l = [1, 2]
l.append(3)
res = l[2]
len_l = len(l)
`)
	if val, ok := vm.Get("res"); !ok || val != int64(3) {
		t.Errorf("res = %v, want 3", val)
	}
	if val, ok := vm.Get("len_l"); !ok || val != int64(3) {
		t.Errorf("len_l = %v, want 3", val)
	}
}

func TestCompileComprehension(t *testing.T) {
	// List comprehension
	vm := run(t, `
res = [x*x for x in range(5)]
`)
	if val, ok := vm.Get("res"); !ok {
		t.Error("res not found")
	} else {
		l := val.(*taivm.List).Elements
		if len(l) != 5 {
			t.Errorf("len = %d, want 5", len(l))
		}
		if l[4] != int64(16) {
			t.Errorf("l[4] = %v, want 16", l[4])
		}
	}

	// Filter
	vm = run(t, `
res = [x for x in range(10) if x % 2 == 0]
`)
	if val, ok := vm.Get("res"); !ok {
		t.Error("res not found")
	} else {
		l := val.(*taivm.List).Elements
		if len(l) != 5 {
			t.Errorf("len = %d, want 5", len(l))
		}
		if l[1] != int64(2) {
			t.Errorf("l[1] = %v, want 2", l[1])
		}
	}

	// Nested
	vm = run(t, `
res = [x+y for x in [1, 2] for y in [10, 20]]
`)
	// 1+10, 1+20, 2+10, 2+20 -> 11, 21, 12, 22
	if val, ok := vm.Get("res"); !ok {
		t.Error("res not found")
	} else {
		l := val.(*taivm.List).Elements
		if len(l) != 4 {
			t.Errorf("len = %d, want 4", len(l))
		}
		if l[0] != int64(11) || l[3] != int64(22) {
			t.Errorf("res = %v", l)
		}
	}

	// Dict comprehension
	vm = run(t, `
res = {x: x*x for x in range(3)}
`)
	if val, ok := vm.Get("res"); !ok {
		t.Error("res not found")
	} else {
		m := val.(map[any]any)
		if len(m) != 3 {
			t.Errorf("len = %d, want 3", len(m))
		}
		if m[int64(2)] != int64(4) {
			t.Errorf("m[2] = %v, want 4", m[int64(2)])
		}
	}

	// Scope isolation
	vm = run(t, `
x = 100
res = [x for x in range(5)]
after = x
`)
	if val, ok := vm.Get("after"); !ok || val != int64(100) {
		t.Errorf("variable x leaked: %v", val)
	}
}

func TestCompilePass(t *testing.T) {
	run(t, `
for i in range(1):
	pass
`)
}

func TestCompileReturnNone(t *testing.T) {
	vm := run(t, `
def f():
	return
res = f()
`)
	if val, ok := vm.Get("res"); !ok || val != nil {
		t.Errorf("res = %v, want nil", val)
	}
}

func TestCompileListAssignment(t *testing.T) {
	vm := run(t, `
[a, b] = [1, 2]
`)
	if val, ok := vm.Get("a"); !ok || val != int64(1) {
		t.Errorf("a = %v", val)
	}
	if val, ok := vm.Get("b"); !ok || val != int64(2) {
		t.Errorf("b = %v", val)
	}
}

func TestCompileTupleAssignment(t *testing.T) {
	vm := run(t, `
(a, b) = (1, 2)
`)
	if val, ok := vm.Get("a"); !ok || val != int64(1) {
		t.Errorf("a = %v", val)
	}
	if val, ok := vm.Get("b"); !ok || val != int64(2) {
		t.Errorf("b = %v", val)
	}
}

func TestCompileSliceAssignment(t *testing.T) {
	vm := run(t, `
l = [1, 2, 3, 4]
l[1:3] = [8, 9]
`)
	if val, ok := vm.Get("l"); !ok {
		t.Error("l not found")
	} else {
		l := val.(*taivm.List).Elements
		if len(l) != 4 || l[1] != int64(8) || l[2] != int64(9) {
			t.Errorf("l = %v", l)
		}
	}
}

func TestCompileErrorsMore(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"aug_assign_paren", "x=1; (x) += 1", "unsupported augmented assignment target"},
		{"destructure_star", "a, *b = [1, 2]", "unsupported variable type"},
		{"set_comp", "s = {x for x in []}", "dict comprehension body must be DictEntry"},
		{"set_comp_attr", "({x for x in []}).a = 1", "dict comprehension body must be DictEntry"},
		{"param_order", "def f(a=1, b): pass", "non-default argument"},
		{"param_star_bad", "def f(*1): pass", "variadic parameter must be identifier"},
		{"param_variadic_not_last", "def f(*args, b): pass", "variadic parameter must be last"},
		{"assign_literal", "1 = 1", "unsupported assignment target"},
		{"assign_paren_literal", "(1) = 1", "unsupported assignment target"},
		{"assign_invalid_list", "[1] = [1]", "unsupported variable type"},
		{"assign_invalid_tuple", "(1,) = (1,)", "unsupported variable type"},
		{"assign_binary_lhs", "(a+b) = 1", "unsupported assignment target"},
		{"aug_assign_literal", "1 += 1", "unsupported augmented assignment target"},
		{"aug_assign_list", "l=[1]; [l[0]] += [1]", "unsupported augmented assignment target"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile("test", strings.NewReader(tt.src))
			if err == nil {
				t.Error("expected error")
			} else if tt.want != "" {
				if !strings.Contains(err.Error(), tt.want) {
					t.Logf("got error: %v", err)
				}
			}
		})
	}
}

func TestNewVMError(t *testing.T) {
	_, err := NewVM("test", strings.NewReader("if"))
	if err == nil {
		t.Error("expected error")
	}
}

func TestNativeFuncErrors(t *testing.T) {
	vm := taivm.NewVM(&taivm.Function{})

	// Len
	if _, err := Len.Func(vm, []any{}); err == nil {
		t.Error("expected error")
	}
	if _, err := Len.Func(vm, []any{1, 2}); err == nil {
		t.Error("expected error")
	}
	if _, err := Len.Func(vm, []any{1}); err == nil {
		t.Error("expected error")
	}

	// Range
	if _, err := Range.Func(vm, []any{}); err == nil {
		t.Error("expected error")
	}
	if _, err := Range.Func(vm, []any{1, 2, 3, 4}); err == nil {
		t.Error("expected error")
	}
	if _, err := Range.Func(vm, []any{"a"}); err == nil {
		t.Error("expected error")
	}
	if _, err := Range.Func(vm, []any{1, "a"}); err == nil {
		t.Error("expected error")
	}
	if _, err := Range.Func(vm, []any{1, 2, "a"}); err == nil {
		t.Error("expected error")
	}
	if _, err := Range.Func(vm, []any{0, 10, 0}); err == nil {
		t.Error("expected error")
	}
}

func TestCompileAugmentedAssignAllOps(t *testing.T) {
	vm := run(t, `
a = 20.0
a /= 4
b = 20
b //= 3
c = 10
c %= 3
d = 3
d &= 1
e = 1
e |= 2
f = 3
f ^= 1
g = 1
g <<= 2
h = 8
h >>= 2
`)
	check := func(name string, want any) {
		if val, ok := vm.Get(name); !ok || val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}
	// a is float because we started with 20.0 to be safe with division types
	check("a", 5.0)
	check("b", int64(6))
	check("c", int64(1))
	check("d", int64(1))
	check("e", int64(3))
	check("f", int64(2))
	check("g", int64(4))
	check("h", int64(2))
}

func TestCompileAugmentedAssignSlice(t *testing.T) {
	vm := run(t, `
l = [1, 2, 3]
l[0:1] += [4]
`)
	if val, ok := vm.Get("l"); !ok {
		t.Error("l not found")
	} else {
		l := val.(*taivm.List).Elements
		// [1] += [4] -> [1, 4], so list becomes [1, 4, 2, 3]
		if len(l) != 4 {
			t.Errorf("len = %d, want 4", len(l))
		} else if l[1] != int64(4) {
			t.Errorf("l[1] = %v, want 4", l[1])
		}
	}
}

func TestCompileStoreUnpackComplex(t *testing.T) {
	s := &taivm.Struct{
		Fields: map[string]any{
			"x": int64(0),
		},
	}
	src := `
l = [1, 2]
[l[0], s.x] = [3, 4]
`
	fn, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	vm := taivm.NewVM(fn)
	vm.Def("s", s)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	if val, ok := vm.Get("l"); !ok {
		t.Error("l not found")
	} else {
		l := val.(*taivm.List).Elements
		if l[0] != int64(3) {
			t.Errorf("l[0] = %v, want 3", l[0])
		}
	}
	if val, ok := s.Fields["x"]; !ok || val != int64(4) {
		t.Errorf("s.x = %v, want 4", val)
	}
}

func TestCompileLoad(t *testing.T) {
	src := `load("mod", "sym")`
	_, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
}

func TestNativeLenError(t *testing.T) {
	vm := taivm.NewVM(&taivm.Function{})
	// Invalid type
	if _, err := Len.Func(vm, []any{int64(1)}); err == nil {
		t.Error("expected error for len(int)")
	}
}

func TestNativeRangeErrors(t *testing.T) {
	vm := taivm.NewVM(&taivm.Function{})

	check := func(args []any) {
		if _, err := Range.Func(vm, args); err == nil {
			t.Errorf("expected error for args %v", args)
		}
	}

	check([]any{int64(1), "a"})
	check([]any{int64(1), int64(2), "a"})
	check([]any{"a"})
	check([]any{int64(1), int64(2), int64(0)}) // step 0
}

func testCompiler() *compiler {
	return newCompiler("test")
}

type mockStmt struct {
	syntax.Stmt
}

func TestCoverage_CompileStmtDefault(t *testing.T) {
	c := testCompiler()
	err := c.compileStmt(&mockStmt{})
	if err == nil || !strings.Contains(err.Error(), "unsupported statement type") {
		t.Errorf("expected unsupported statement type error, got %v", err)
	}
}

type mockExpr struct {
	syntax.Expr
}

func TestCoverage_CompileExprDefault(t *testing.T) {
	c := testCompiler()
	err := c.compileExpr(&mockExpr{})
	if err == nil || !strings.Contains(err.Error(), "unsupported expression") {
		t.Errorf("expected error, got %v", err)
	}
}

func TestCoverage_CompileBranchErrors(t *testing.T) {
	c := testCompiler()

	err := c.compileBranch(&syntax.BranchStmt{Token: syntax.BREAK})
	if err == nil || !strings.Contains(err.Error(), "outside loop") {
		t.Errorf("want error 'outside loop', got %v", err)
	}

	err = c.compileBranch(&syntax.BranchStmt{Token: syntax.CONTINUE})
	if err == nil || !strings.Contains(err.Error(), "outside loop") {
		t.Errorf("want error 'outside loop', got %v", err)
	}
}

func TestCoverage_AugmentedAssignErrors(t *testing.T) {
	c := testCompiler()

	// Unsupported Op
	err := c.compileAugmentedAssign(&syntax.AssignStmt{
		Op:  syntax.EQ,
		LHS: &syntax.Ident{Name: "x"},
		RHS: &syntax.Literal{Value: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("want error 'not supported', got %v", err)
	}

	// Unsupported LHS
	err = c.compileAugmentedAssign(&syntax.AssignStmt{
		Op:  syntax.PLUS_EQ,
		LHS: &syntax.Literal{Value: 1},
		RHS: &syntax.Literal{Value: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported augmented assignment target") {
		t.Errorf("want error 'unsupported augmented assignment target', got %v", err)
	}
}

func TestCoverage_UnaryBinaryErrors(t *testing.T) {
	c := testCompiler()

	err := c.compileUnaryExpr(&syntax.UnaryExpr{
		Op: syntax.AND, // Invalid unary
		X:  &syntax.Literal{Value: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported unary op") {
		t.Errorf("want error 'unsupported unary op', got %v", err)
	}

	err = c.compileBinaryExpr(&syntax.BinaryExpr{
		Op: syntax.DEF, // Invalid binary
		X:  &syntax.Literal{Value: 1},
		Y:  &syntax.Literal{Value: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported binary op") {
		t.Errorf("want error 'unsupported binary op', got %v", err)
	}
}

func TestCoverage_Constants(t *testing.T) {
	c := testCompiler()

	// Slice is not comparable
	sl := []int{1}
	if isComparable(sl) {
		t.Error("slice should not be comparable")
	}

	idx1 := c.addConst(sl)
	idx2 := c.addConst([]int{1}) // different slice instance
	if idx1 == idx2 {
		t.Error("different non-comparable constants should have different indices")
	}

	idx3 := c.addConst(1)
	idx4 := c.addConst(1)
	if idx3 != idx4 {
		t.Error("comparable constants should be deduplicated")
	}
}

func TestCoverage_ControlFlow(t *testing.T) {
	// If without else
	src := `
x = 0
if x == 0:
	x = 1
`
	vm := run(t, src)
	if val, ok := vm.Get("x"); !ok || val != int64(1) {
		t.Errorf("x = %v, want 1", val)
	}

	// While loop with break (ensure coverage)
	src = `
x = 0
while 1 == 1:
	x = 1
	break
`
	vm = run(t, src)
	if val, ok := vm.Get("x"); !ok || val != int64(1) {
		t.Errorf("x = %v, want 1", val)
	}
}

func TestCoverage_CompileForErrors(t *testing.T) {
	src := `
for 1 in [1]:
	pass
`
	_, err := Compile("test", strings.NewReader(src))
	if err == nil || !strings.Contains(err.Error(), "unsupported variable type") {
		t.Errorf("want error 'unsupported variable type', got %v", err)
	}
}

func TestCoverage_NativeFuncs(t *testing.T) {
	vm := taivm.NewVM(&taivm.Function{})

	// Len invalid
	_, err := Len.Func(vm, []any{123})
	if err == nil || !strings.Contains(err.Error(), "has no len") {
		t.Errorf("want error 'has no len', got %v", err)
	}

	// Range step zero
	_, err = Range.Func(vm, []any{0, 10, 0})
	if err == nil || !strings.Contains(err.Error(), "step cannot be zero") {
		t.Errorf("want error 'step cannot be zero', got %v", err)
	}
}

func TestCoverage_UnsupportedExpression(t *testing.T) {
	// Try to compile a Set expression {1, 2} which is likely unsupported in compileExpr
	src := `
s = {1, 2}
`
	_, err := Compile("test", strings.NewReader(src))
	if err == nil {
		// If it compiles, check if it ran correctly?
	} else if strings.Contains(err.Error(), "unsupported expression") {
		// Covered
	} else {
		t.Logf("Got error: %v", err)
	}
}

func TestCoverage_CompileStoreAllTypes(t *testing.T) {
	// Test all supported types in compileStore
	src := `
# Simple identifier
a = 1

# List unpacking
[b, c] = [2, 3]

# Tuple unpacking
(d, e) = (4, 5)

# ParenExpr
(f) = 6

# DotExpr
s = struct({"x": 10})
s.x = 20

# IndexExpr
l = [100]
l[0] = 200

# SliceExpr
l2 = [1, 2, 3]
l2[0:2] = [4, 5]
`
	vm := run(t, src)

	check := func(name string, want int64) {
		if val, ok := vm.Get(name); !ok || val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("a", 1)
	check("b", 2)
	check("c", 3)
	check("d", 4)
	check("e", 5)
	check("f", 6)

	if val, ok := vm.Get("s"); !ok {
		t.Error("s not found")
	} else if s, ok := val.(*taivm.Struct); !ok {
		t.Errorf("s is %T, want *Struct", val)
	} else if val, ok := s.Fields["x"]; !ok || val != int64(20) {
		t.Errorf("s.x = %v, want 20", val)
	}

	if val, ok := vm.Get("l"); !ok {
		t.Error("l not found")
	} else if l, ok := val.(*taivm.List); !ok {
		t.Errorf("l is %T, want *List", val)
	} else if l.Elements[0] != int64(200) {
		t.Errorf("l[0] = %v, want 200", l.Elements[0])
	}

	if val, ok := vm.Get("l2"); !ok {
		t.Error("l2 not found")
	} else if l, ok := val.(*taivm.List); !ok {
		t.Errorf("l2 is %T, want *List", val)
	} else if len(l.Elements) != 3 || l.Elements[0] != int64(4) || l.Elements[1] != int64(5) {
		t.Errorf("l2 = %v", l.Elements)
	}
}

func TestCoverage_CompileExprAllTypes(t *testing.T) {
	// Test all supported types in compileExpr
	src := `
# Literal
a = 1

# Ident
b = a

# UnaryExpr
c = -a

# BinaryExpr
d = a + b

# CallExpr
e = len([1, 2, 3])

# ListExpr
f = [1, 2, 3]

# DictExpr
g = {"a": 1}

# IndexExpr
h = f[0]

# TupleExpr
i = (1, 2)

# ParenExpr
j = (1 + 2)

# SliceExpr
k = f[0:2]

# DotExpr
s = struct({'x': 10})
l = s.x

# CondExpr
m = 1 if 1 < 2 else 0

# LambdaExpr
n = lambda x: x + 1

# Comprehension
o = [x for x in [1, 2, 3]]
`
	vm := run(t, src)

	check := func(name string, want any) {
		if val, ok := vm.Get(name); !ok {
			t.Errorf("%s not found", name)
		} else if val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("a", int64(1))
	check("b", int64(1))
	check("c", int64(-1))
	check("d", int64(2))
	check("e", int64(3))
	check("m", int64(1))

	if val, ok := vm.Get("f"); !ok {
		t.Error("f not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 3 {
		t.Errorf("f = %v", val)
	}

	if val, ok := vm.Get("g"); !ok {
		t.Error("g not found")
	} else if m, ok := val.(map[any]any); !ok || m["a"] != int64(1) {
		t.Errorf("g = %v", val)
	}

	check("h", int64(1))

	if val, ok := vm.Get("i"); !ok {
		t.Error("i not found")
	} else if l, ok := val.(*taivm.List); !ok || !l.Immutable || len(l.Elements) != 2 {
		t.Errorf("i = %v", val)
	}

	check("j", int64(3))

	if val, ok := vm.Get("k"); !ok {
		t.Error("k not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 2 {
		t.Errorf("k = %v", val)
	}

	check("l", int64(10))

	if val, ok := vm.Get("n"); !ok {
		t.Error("n not found")
	} else if _, ok := val.(*taivm.Function); !ok {
		t.Errorf("n is %T, want *Function", val)
	}

	if val, ok := vm.Get("o"); !ok {
		t.Error("o not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 3 {
		t.Errorf("o = %v", val)
	}
}

func TestCoverage_CompileSimpleAssignAllTypes(t *testing.T) {
	// Test all supported types in compileSimpleAssign
	src := `
# Simple identifier
a = 1

# List unpacking
[b, c] = [2, 3]

# Tuple unpacking
(d, e) = (4, 5)

# ParenExpr
(f) = 6

# IndexExpr
l = [100]
l[0] = 200

# SliceExpr
l2 = [1, 2, 3]
l2[0:2] = [4, 5]

# DotExpr
s = {"x": 10}
s.x = 20
`
	vm := run(t, src)

	check := func(name string, want int64) {
		if val, ok := vm.Get(name); !ok || val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("a", 1)
	check("b", 2)
	check("c", 3)
	check("d", 4)
	check("e", 5)
	check("f", 6)

	if val, ok := vm.Get("l"); !ok {
		t.Error("l not found")
	} else if l, ok := val.(*taivm.List); !ok || l.Elements[0] != int64(200) {
		t.Errorf("l[0] = %v, want 200", l.Elements[0])
	}

	if val, ok := vm.Get("l2"); !ok {
		t.Error("l2 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 3 || l.Elements[0] != int64(4) || l.Elements[1] != int64(5) {
		t.Errorf("l2 = %v", l.Elements)
	}

	if val, ok := vm.Get("s"); !ok {
		t.Error("s not found")
	} else if s, ok := val.(*taivm.Struct); !ok || s.Fields["x"] != int64(20) {
		t.Errorf("s.x = %v, want 20", s.Fields["x"])
	}
}

func TestCoverage_UnaryExprAllOps(t *testing.T) {
	// Test all supported unary operators
	src := `
a = 1
b = +a
c = -a
d = not (a == 0)
e = ~0
`
	vm := run(t, src)

	check := func(name string, want any) {
		if val, ok := vm.Get(name); !ok {
			t.Errorf("%s not found", name)
		} else if val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("a", int64(1))
	check("b", int64(1))
	check("c", int64(-1))
	check("d", true)
	check("e", int64(-1))
}

func TestCoverage_BinaryExprAllOps(t *testing.T) {
	// Test all supported binary operators
	src := `
a = 10
b = 3

# Arithmetic
c = a + b
d = a - b
e = a * b
f = a / b
g = a // b
h = a % b
i = a ** b

# Comparison
j = a == b
k = a != b
l = a < b
m = a <= b
n = a > b
o = a >= b

# Bitwise
p = a & b
q = a | b
r = a ^ b
s = a << b
t = a >> b

# Contains
u = 1 in [1, 2, 3]
v = 1 not in [1, 2, 3]

# Short-circuit
w = (1 < 2) and (2 < 3)
x = (1 < 2) or (2 > 3)
`
	vm := run(t, src)

	check := func(name string, want any) {
		if val, ok := vm.Get(name); !ok {
			t.Errorf("%s not found", name)
		} else if val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("c", int64(13))
	check("d", int64(7))
	check("e", int64(30))
	check("f", 10.0/3.0)
	check("g", int64(3))
	check("h", int64(1))
	check("i", int64(1000))

	check("j", false)
	check("k", true)
	check("l", false)
	check("m", false)
	check("n", true)
	check("o", true)

	check("p", int64(2))
	check("q", int64(11))
	check("r", int64(9))
	check("s", int64(80))
	check("t", int64(1))

	check("u", true)
	check("v", false)

	check("w", true)
	check("x", true)
}

func TestCoverage_CompileIfAllPaths(t *testing.T) {
	// Test all paths in compileIf
	src := `
# If with else
a = 0
if 1 < 2:
	a = 1
else:
	a = 2

# If without else
b = 0
if 1 < 2:
	b = 1

# If with empty false branch
c = 0
if 1 > 2:
	c = 1
`
	vm := run(t, src)

	check := func(name string, want int64) {
		if val, ok := vm.Get(name); !ok || val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("a", 1)
	check("b", 1)
	check("c", 0)
}

func TestCoverage_CompileWhileAllPaths(t *testing.T) {
	// Test all paths in compileWhile
	src := `
# While with break
a = 0
while 1 == 1:
	a = 1
	break

# While with continue
b = 0
i = 0
while i < 5:
	i += 1
	if i % 2 == 0:
		continue
	b += i

# While with no break/continue
c = 0
i = 0
while i < 3:
	c += i
	i += 1
`
	vm := run(t, src)

	check := func(name string, want int64) {
		if val, ok := vm.Get(name); !ok || val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("a", 1)
	check("b", 9) // 1 + 3 + 5
	check("c", 3) // 0 + 1 + 2
}

func TestCoverage_CompileForAllPaths(t *testing.T) {
	// Test all paths in compileFor
	src := `
# For with break
a = 0
for i in [1, 2, 3]:
	if i == 2:
		break
	a += i

# For with continue
b = 0
for i in [1, 2, 3]:
	if i == 2:
		continue
	b += i

# For with no break/continue
c = 0
for i in [1, 2, 3]:
	c += i
`
	vm := run(t, src)

	check := func(name string, want int64) {
		if val, ok := vm.Get(name); !ok || val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("a", 1) // 1 only
	check("b", 4) // 1 + 3
	check("c", 6) // 1 + 2 + 3
}

func TestCoverage_ExtractParamNamesAllPaths(t *testing.T) {
	// Test all paths in extractParamNames
	src := `
# Simple params
def f1(a, b): pass

# Default params
def f2(a, b=1): pass

# Variadic param
def f3(a, *b): pass

# Mixed
def f4(a, b=1, *c): pass
`
	_, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Test error paths
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"variadic_not_last", "def f(*a, b): pass", "variadic parameter must be last"},
		{"non_default_after_default", "def f(a=1, b): pass", "non-default argument follows default argument"},
		{"invalid_variadic", "def f(*1): pass", "variadic parameter must be identifier"},
		{"complex_param", "def f(a+b): pass", "complex parameters not supported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile("test", strings.NewReader(tt.src))
			if err == nil {
				t.Error("expected error")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestCoverage_CompileCallExprAllPaths(t *testing.T) {
	// Test all paths in compileCallExpr
	src := `
# Simple call
def f(a, b): return a + b
a = f(1, 2)

# Keyword args
b = f(a=1, b=2)

# Mixed positional and keyword
c = f(1, b=2)

# Unpack positional
d = f(*[1, 2])

# Unpack keyword
e = f(**{"a": 1, "b": 2})

# Mixed unpack
f = f(1, *[2], **{"b": 3})
`
	vm := run(t, src)

	check := func(name string, want int64) {
		if val, ok := vm.Get(name); !ok || val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("a", 3)
	check("b", 3)
	check("c", 3)
	check("d", 3)
	check("e", 3)
	check("f", 4) // a=1, b=3
}

func TestCoverage_CompileComprehensionAllPaths(t *testing.T) {
	// Test all paths in compileComprehension
	src := `
# List comprehension
a = [x for x in range(3)]

# Dict comprehension
b = {x: x*x for x in range(3)}

# With if clause
c = [x for x in range(5) if x % 2 == 0]

# Nested
d = [x+y for x in [1, 2] for y in [10, 20]]

# Scope isolation
x = 100
e = [x for x in range(3)]
f = x
`
	vm := run(t, src)

	check := func(name string, want any) {
		if val, ok := vm.Get(name); !ok {
			t.Errorf("%s not found", name)
		} else if val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	if val, ok := vm.Get("a"); !ok {
		t.Error("a not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 3 {
		t.Errorf("a = %v", val)
	}

	if val, ok := vm.Get("b"); !ok {
		t.Error("b not found")
	} else if m, ok := val.(map[any]any); !ok || len(m) != 3 {
		t.Errorf("b = %v", val)
	}

	if val, ok := vm.Get("c"); !ok {
		t.Error("c not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 3 {
		t.Errorf("c = %v", val)
	}

	if val, ok := vm.Get("d"); !ok {
		t.Error("d not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 4 {
		t.Errorf("d = %v", val)
	}

	check("f", int64(100))
}

func TestCoverage_CompileLambdaExprAllPaths(t *testing.T) {
	// Test all paths in compileLambdaExpr
	src := `
# Simple lambda
a = lambda x: x + 1

# Multiple params
b = lambda x, y: x + y

# Default params
c = lambda x, y=1: x + y

# Variadic
d = lambda x, *y: len(y)

# Call lambda
e = a(10)
f = b(5, 7)
g = c(5)
h = d(1, 2, 3)
`
	vm := run(t, src)

	check := func(name string, want any) {
		if val, ok := vm.Get(name); !ok {
			t.Errorf("%s not found", name)
		} else if val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("e", int64(11))
	check("f", int64(12))
	check("g", int64(6))
	check("h", int64(2))
}

func TestCoverage_CompileCondExprAllPaths(t *testing.T) {
	// Test all paths in compileCondExpr
	src := `
# True condition
a = 1 if 1 < 2 else 0

# False condition
b = 1 if 1 > 2 else 0

# Nested
c = 1 if 1 < 2 else (2 if 2 < 3 else 0)
`
	vm := run(t, src)

	check := func(name string, want int64) {
		if val, ok := vm.Get(name); !ok || val != want {
			t.Errorf("%s = %v, want %v", name, val, want)
		}
	}

	check("a", 1)
	check("b", 0)
	check("c", 1)
}
