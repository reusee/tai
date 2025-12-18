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
	if val, ok := vm.Get("a"); !ok || val != 3 {
		t.Errorf("a = %v, want 3", val)
	}
	if val, ok := vm.Get("b"); !ok || val != 9 {
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
	if val, ok := vm.Get("res"); !ok || val != 1 {
		t.Errorf("res = %v, want 1", val)
	}

	vm = run(t, `
res = 0
if 1 > 2:
	res = 1
else:
	res = 2
`)
	if val, ok := vm.Get("res"); !ok || val != 2 {
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
	if val, ok := vm.Get("sum"); !ok || val != 10 {
		t.Errorf("sum = %v, want 10", val)
	}
}

func TestCompileFunction(t *testing.T) {
	vm := run(t, `
def add(a, b):
	return a + b
res = add(3, 4)
`)
	if val, ok := vm.Get("res"); !ok || val != 7 {
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
	if val, ok := vm.Get("res"); !ok || val != 55 {
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
	if val, ok := vm.Get("res"); !ok || val != 2 {
		t.Errorf("res = %v, want 2", val)
	}
	if val, ok := vm.Get("res2"); !ok || val != 5 {
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
	if val, ok := vm.Get("res"); !ok || val != 1 {
		t.Errorf("res = %v, want 1", val)
	}
	if val, ok := vm.Get("res2"); !ok || val != 3 {
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
	if val, ok := vm.Get("sum"); !ok || val != 9 {
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
	if val, ok := vm.Get("res"); !ok || val != 8 {
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
			return args[0].(int) + args[1].(int), nil
		},
	})

	vm.Run(func(intr *taivm.Interrupt, err error) bool {
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		return false
	})

	if val, ok := vm.Get("res"); !ok || val != 30 {
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
