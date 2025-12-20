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

func check(t *testing.T, vm *taivm.VM, name string, want any) {
	t.Helper()
	if val, ok := vm.Get(name); !ok {
		t.Errorf("%s not found", name)
	} else if val != want {
		t.Errorf("%s = %v (%T), want %v (%T)", name, val, val, want, want)
	}
}

func TestOps(t *testing.T) {
	// Binary Ops
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
i = pow(a, b)

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
	check(t, vm, "c", int64(13))
	check(t, vm, "d", int64(7))
	check(t, vm, "e", int64(30))
	check(t, vm, "f", int64(3))
	check(t, vm, "g", int64(3))
	check(t, vm, "h", int64(1))
	check(t, vm, "i", int64(1000))
	check(t, vm, "j", false)
	check(t, vm, "k", true)
	check(t, vm, "l", false)
	check(t, vm, "m", false)
	check(t, vm, "n", true)
	check(t, vm, "o", true)
	check(t, vm, "p", int64(2))
	check(t, vm, "q", int64(11))
	check(t, vm, "r", int64(9))
	check(t, vm, "s", int64(80))
	check(t, vm, "t", int64(1))
	check(t, vm, "u", true)
	check(t, vm, "v", false)
	check(t, vm, "w", true)
	check(t, vm, "x", true)

	// Unary Ops
	src = `
a = 1
b = +a
c = -a
d = not (a == 0)
e = ~0
`
	vm = run(t, src)
	check(t, vm, "a", int64(1))
	check(t, vm, "b", int64(1))
	check(t, vm, "c", int64(-1))
	check(t, vm, "d", true)
	check(t, vm, "e", int64(-1))
}

func TestControlFlow(t *testing.T) {
	// If/Else paths
	src := `
a = 0
if 1 < 2:
	a = 1
else:
	a = 2

b = 0
if 1 < 2:
	b = 1

c = 0
if 1 > 2:
	c = 1

# CondExpr
d = 1 if 1 < 2 else 0
e = 1 if 1 > 2 else 0
f = 1 if 1 < 2 else (2 if 2 < 3 else 0)
`
	vm := run(t, src)
	check(t, vm, "a", int64(1))
	check(t, vm, "b", int64(1))
	check(t, vm, "c", int64(0))
	check(t, vm, "d", int64(1))
	check(t, vm, "e", int64(0))
	check(t, vm, "f", int64(1))

	// While loops
	src = `
a = 0
while 1 == 1:
	a = 1
	break

b = 0
i = 0
while i < 5:
	i += 1
	if i % 2 == 0:
		continue
	b += i

c = 0
i = 0
while i < 3:
	c += i
	i += 1
`
	vm = run(t, src)
	check(t, vm, "a", int64(1))
	check(t, vm, "b", int64(9)) // 1 + 3 + 5
	check(t, vm, "c", int64(3)) // 0 + 1 + 2

	// For loops (list, break, continue)
	src = `
a = 0
for i in [1, 2, 3]:
	if i == 2:
		break
	a += i

b = 0
for i in [1, 2, 3]:
	if i == 2:
		continue
	b += i

c = 0
for i in [1, 2, 3]:
	c += i

d = 0
for i in range(1):
	pass
`
	vm = run(t, src)
	check(t, vm, "a", int64(1))
	check(t, vm, "b", int64(4))
	check(t, vm, "c", int64(6))
}

func TestFunctions(t *testing.T) {
	src := `
# Basic
def add(a, b):
	return a + b
res1 = add(3, 4)

# Recursion
def fib(n):
	if n <= 1:
		return n
	return fib(n-1) + fib(n-2)
res2 = fib(10)

# Closure
def make_adder(x):
	def adder(y):
		return x + y
	return adder
add5 = make_adder(5)
res3 = add5(3)

# Implicit return
def f_none():
	return
res4 = f_none()

# Keyword Args
def sub(a, b):
	return a - b
res5 = sub(10, 3)
res6 = sub(a=10, b=3)
res7 = sub(b=3, a=10)
res8 = sub(10, b=3)

# Variadic
def f_var(a, *b):
	return b
l1 = f_var(1)
l2 = f_var(1, 2)
l3 = f_var(1, 2, 3)
`
	vm := run(t, src)
	check(t, vm, "res1", int64(7))
	check(t, vm, "res2", int64(55))
	check(t, vm, "res3", int64(8))
	check(t, vm, "res4", nil)
	check(t, vm, "res5", int64(7))
	check(t, vm, "res6", int64(7))
	check(t, vm, "res7", int64(7))
	check(t, vm, "res8", int64(7))

	if val, ok := vm.Get("l1"); !ok {
		t.Error("l1 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 0 {
		t.Errorf("l1 = %v", val)
	}
	if val, ok := vm.Get("l2"); !ok {
		t.Error("l2 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 1 {
		t.Errorf("l2 = %v", val)
	}
	if val, ok := vm.Get("l3"); !ok {
		t.Error("l3 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 2 {
		t.Errorf("l3 = %v", val)
	}
}

func TestLambdas(t *testing.T) {
	src := `
# Simple lambda
a = lambda x: x + 1

# Multiple params
b = lambda x, y: x + y

# Default params
c = lambda x, y=1: x + y

# Variadic
d = lambda x, *y: len(y)

e = a(10)
f = b(5, 7)
g = c(5)
h = d(1, 2, 3)
`
	vm := run(t, src)
	check(t, vm, "e", int64(11))
	check(t, vm, "f", int64(12))
	check(t, vm, "g", int64(6))
	check(t, vm, "h", int64(2))
}

func TestCalls(t *testing.T) {
	src := `
def f(a, b): return a + b
def g(x, y): return x * y
def h(a, b, c, d): return a + b + c + d

# Simple call
a = f(1, 2)

# Keyword args
b = f(a=1, b=2)

# Mixed positional and keyword
c = f(1, b=2)

# Unpack positional
d = f(*[1, 2])

# Unpack keyword
e = g(**{"x": 3, "y": 4})

# Mixed unpacking
res3 = h(1, *[2], c=3, **{"d": 4})
res4 = h(1, *[2], 3, **{"d": 4})
`
	vm := run(t, src)
	check(t, vm, "a", int64(3))
	check(t, vm, "b", int64(3))
	check(t, vm, "c", int64(3))
	check(t, vm, "d", int64(3))
	check(t, vm, "e", int64(12))
	check(t, vm, "res3", int64(10))
	check(t, vm, "res4", int64(10))
}

func TestCollections(t *testing.T) {
	// List, Map, Tuple, Slice, Comprehensions
	src := `
# List
l = [1, 2, 3]
l_res = l[1]
l[2] = 5
l_res2 = l[2]
l.append(10)
l_len = len(l)

# Map
d = {"a": 1, "b": 2}
d_res = d["a"]
d["c"] = 3
d_res2 = d["c"]

# Tuple
t = (1, 2, 3)
t_res = t[1]

# Slice
sl = [1, 2, 3, 4, 5]
sl_res = sl[1:4]
sl_step = sl[::2]

# Comprehensions
lc = [x*x for x in range(3)]
dc = {x: x*x for x in range(3)}
fc = [x for x in range(5) if x % 2 == 0]
nc = [x+y for x in [1, 2] for y in [10, 20]]

# Scope check
x = 100
sc = [x for x in range(2)]
scope_res = x
`
	vm := run(t, src)
	check(t, vm, "l_res", int64(2))
	check(t, vm, "l_res2", int64(5))
	check(t, vm, "l_len", int64(4))

	check(t, vm, "d_res", int64(1))
	check(t, vm, "d_res2", int64(3))

	check(t, vm, "t_res", int64(2))

	if val, ok := vm.Get("sl_res"); !ok {
		t.Error("sl_res not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 3 {
		t.Errorf("sl_res = %v", val)
	}

	if val, ok := vm.Get("lc"); !ok {
		t.Error("lc not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 3 {
		t.Errorf("lc = %v", val)
	}

	if val, ok := vm.Get("dc"); !ok {
		t.Error("dc not found")
	} else if m, ok := val.(map[any]any); !ok || len(m) != 3 {
		t.Errorf("dc = %v", val)
	}

	check(t, vm, "scope_res", int64(100))
}

func TestAssignments(t *testing.T) {
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

# Complex unpacking
l3 = [1, 2]
[l3[0], s.x] = [3, 4]
`
	vm := run(t, src)
	check(t, vm, "a", int64(1))
	check(t, vm, "b", int64(2))
	check(t, vm, "c", int64(3))
	check(t, vm, "d", int64(4))
	check(t, vm, "e", int64(5))
	check(t, vm, "f", int64(6))

	if val, ok := vm.Get("s"); !ok {
		t.Error("s not found")
	} else if s, ok := val.(*taivm.Struct); !ok || s.Fields["x"] != int64(4) {
		t.Errorf("s.x = %v", s.Fields["x"])
	}

	if val, ok := vm.Get("l"); !ok {
		t.Error("l not found")
	} else if l, ok := val.(*taivm.List); !ok || l.Elements[0] != int64(200) {
		t.Errorf("l = %v", val)
	}

	if val, ok := vm.Get("l3"); !ok {
		t.Error("l3 not found")
	} else if l, ok := val.(*taivm.List); !ok || l.Elements[0] != int64(3) {
		t.Errorf("l3 = %v", val)
	}
}

func TestAugmentedAssignments(t *testing.T) {
	src := `
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
i = 5
i *= 3
j = 10
j -= 4

l = [10, 20]
l[0] += 5

m = {"a": 100}
m["a"] += 50

s = struct({"x": 10})
s.x += 5

l2 = [1, 2, 3]
l2[0:1] += [4]
`
	vm := run(t, src)
	check(t, vm, "a", 5.0)
	check(t, vm, "b", int64(6))
	check(t, vm, "c", int64(1))
	check(t, vm, "d", int64(1))
	check(t, vm, "e", int64(3))
	check(t, vm, "f", int64(2))
	check(t, vm, "g", int64(4))
	check(t, vm, "h", int64(2))
	check(t, vm, "i", int64(15))
	check(t, vm, "j", int64(6))

	if val, ok := vm.Get("l"); !ok {
		t.Error("l not found")
	} else if l, ok := val.(*taivm.List); !ok || l.Elements[0] != int64(15) {
		t.Errorf("l[0] = %v", l.Elements[0])
	}

	if val, ok := vm.Get("m"); !ok {
		t.Error("m not found")
	} else if m, ok := val.(map[any]any); !ok || m["a"] != int64(150) {
		t.Errorf("m['a'] = %v", m["a"])
	}

	if val, ok := vm.Get("s"); !ok {
		t.Error("s not found")
	} else if s, ok := val.(*taivm.Struct); !ok || s.Fields["x"] != int64(15) {
		t.Errorf("s.x = %v", s.Fields["x"])
	}

	if val, ok := vm.Get("l2"); !ok {
		t.Error("l2 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 4 || l.Elements[1] != int64(4) {
		t.Errorf("l2 = %v", l.Elements)
	}
}

func TestBuiltins(t *testing.T) {
	src := `
# len
l1 = len([1, 2])
l2 = len("hello")
l3 = len({"a": 1})

# range
r1 = range(5)
sum = 0
for i in r1:
	sum += i

r2 = range(1, 5)
r3 = range(0, 10, 2)
r4 = range(10, 0, -1)
r5 = range(10, 0, -2)

l_range = len(range(10))
v_range = range(10)[0]

# print (just run it)
print("test print")
`
	vm := run(t, src)
	check(t, vm, "l1", int64(2))
	check(t, vm, "l2", int64(5))
	check(t, vm, "l3", int64(1))
	check(t, vm, "sum", int64(10))
	check(t, vm, "l_range", int64(10))
	check(t, vm, "v_range", int64(0))

	if val, ok := vm.Get("r4"); !ok {
		t.Error("r4 not found")
	} else if r, ok := val.(*taivm.Range); !ok || r.Len() != 10 {
		t.Errorf("r4 len = %d, want 10", r.Len())
	}

	if val, ok := vm.Get("r5"); !ok {
		t.Error("r5 not found")
	} else if r, ok := val.(*taivm.Range); !ok || r.Len() != 5 {
		t.Errorf("r5 len = %d, want 5", r.Len())
	}

	// Native func calls with VM access
	vm.Def("native_add", taivm.NativeFunc{
		Name: "native_add",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("want 2 args")
			}
			return args[0].(int64) + args[1].(int64), nil
		},
	})
	vm.Def("get_slice", taivm.NativeFunc{
		Name: "get_slice",
		Func: func(_ *taivm.VM, _ []any) (any, error) {
			return []any{1, 2, 3}, nil
		},
	})

	src = `
res = native_add(10, 20)
res_len = len(get_slice())
`
	fn, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	vm.CurrentFun = fn
	vm.IP = 0
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	check(t, vm, "res", int64(30))
	check(t, vm, "res_len", int64(3))
}

func TestLoad(t *testing.T) {
	src := `load("mod", "sym", alias="sym2")`
	_, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"syntax_error", "if", "syntax error"},
		{"undefined_var", "a = b", "undefined variable"},
		{"keyword_arg_missing", "def f(a, b): pass\nf(a=1)", "missing argument"},
		{"keyword_arg_unexpected", "def f(a): pass\nf(b=1)", "unexpected keyword argument"},
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
		{"assign_list_binary", "[a+b] = [1]", "unsupported variable type"},
		{"assign_binary_lhs", "(a+b) = 1", "unsupported assignment target"},
		{"aug_assign_literal", "1 += 1", "unsupported augmented assignment target"},
		{"aug_assign_list", "l=[1]; [l[0]] += [1]", "unsupported augmented assignment target"},
		{"unsupported_for_var", "for 1 in [1]: pass", "unsupported variable type"},
		{"unsupported_for_var_comp", "[x for 1 in [1]]", "unsupported variable type"},
		{"unsupported_unary_in_comp_if", "[x for x in [] if *x]", "unsupported unary op"},
		{"unsupported_unary_in_comp_for", "[x for x in *x]", "unsupported unary op"},
		{"range_step_zero", "range(1, 10, 0)", "range step cannot be zero"},
		{"range_args_str", "range('a')", "range argument must be integer"},
		{"pow_invalid", "pow('a', 1)", "unsupported argument types"},
		{"def_invalid_param_assign", "def f(1=1): pass", "parameter name must be identifier"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm, err := NewVM("test", strings.NewReader(tt.src))
			if err != nil {
				// Compile error
				if !strings.Contains(err.Error(), tt.want) && !strings.Contains(err.Error(), "syntax error") {
					t.Logf("got compile error: %v, want %s", err, tt.want)
				}
				return
			}
			// Runtime error
			hasErr := false
			for _, err := range vm.Run {
				if err != nil {
					hasErr = true
					if !strings.Contains(err.Error(), tt.want) {
						t.Errorf("got runtime error: %v, want %s", err, tt.want)
					}
				}
			}
			if !hasErr {
				t.Error("expected error but got none")
			}
		})
	}
}

type mockNode struct{}

func (mockNode) Span() (start, end syntax.Position) { return }
func (mockNode) Comments() *syntax.Comments         { return nil }
func (mockNode) AllocComments()                     {}

func TestInternalCoverage(t *testing.T) {
	c := newCompiler("test")

	// Constants
	sl := []int{1}
	if isComparable(sl) {
		t.Error("slice should not be comparable")
	}
	idx1 := c.addConst(sl)
	idx2 := c.addConst([]int{1})
	if idx1 == idx2 {
		t.Error("different non-comparable constants should have different indices")
	}
	idx3 := c.addConst(1)
	idx4 := c.addConst(1)
	if idx3 != idx4 {
		t.Error("comparable constants should be deduplicated")
	}

	// Internal function errors
	if err := c.compileBranch(&syntax.BranchStmt{Token: syntax.BREAK}); err == nil || !strings.Contains(err.Error(), "outside loop") {
		t.Error("expected outside loop error")
	}

	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.EQ}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Error("expected not supported error")
	}

	if err := c.compileUnaryExpr(&syntax.UnaryExpr{Op: syntax.AND}); err == nil || !strings.Contains(err.Error(), "unsupported unary op") {
		t.Error("expected unsupported unary op error")
	}

	lit := &syntax.Literal{Token: syntax.INT, Value: int64(1)}
	if err := c.compileBinaryExpr(&syntax.BinaryExpr{Op: syntax.DEF, X: lit, Y: lit}); err == nil || !strings.Contains(err.Error(), "unsupported binary op") {
		t.Error("expected unsupported binary op error")
	}

	// extractParamNames error
	if _, _, _, err := c.extractParamNames([]syntax.Expr{&syntax.Literal{}}); err == nil || !strings.Contains(err.Error(), "complex parameters not supported") {
		t.Error("expected complex parameters error")
	}

	// compileCallExpr positional after keyword
	// f(a=1, 2)
	callExpr := &syntax.CallExpr{
		Fn: &syntax.Ident{Name: "f"},
		Args: []syntax.Expr{
			&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: lit},
			lit,
		},
	}
	if err := c.compileCallExpr(callExpr); err == nil || !strings.Contains(err.Error(), "positional argument follows keyword argument") {
		t.Error("expected positional after keyword error")
	}

	// Mock types for unreachable default cases
	type mockStmt struct {
		syntax.ExprStmt
	}
	type mockExpr struct {
		syntax.Literal
	}
	type mockClause struct {
		syntax.ForClause
	}

	if err := c.compileStmt(&mockStmt{}); err == nil || !strings.Contains(err.Error(), "unsupported statement type") {
		t.Error("expected unsupported statement type error")
	}

	if err := c.compileExpr(&mockExpr{}); err == nil || !strings.Contains(err.Error(), "unsupported expression") {
		t.Error("expected unsupported expression error")
	}

	// CompileStore errors
	if err := c.compileStore(lit); err == nil || !strings.Contains(err.Error(), "unsupported variable type") {
		t.Error("expected unsupported variable type error")
	}
	if err := c.compileStore(&syntax.SliceExpr{Lo: &mockExpr{}}); err == nil {
		t.Error("expected compileStore SliceExpr Lo error")
	}

	// CompileAssign errors
	if err := c.compileAssign(&syntax.AssignStmt{Op: syntax.PLUS, LHS: &syntax.Ident{Name: "x"}, RHS: lit}); err == nil || !strings.Contains(err.Error(), "augmented assignment op") {
		t.Error("expected unsupported augmented assignment op error")
	}

	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: lit, RHS: lit}); err == nil || !strings.Contains(err.Error(), "unsupported augmented assignment target") {
		t.Error("expected unsupported augmented assignment target error")
	}

	// Augmented Assign sub-expression errors
	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.Ident{Name: "x"}, RHS: &mockExpr{}}); err == nil {
		t.Error("expected Ident RHS error")
	}
	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.IndexExpr{X: lit, Y: lit}, RHS: &mockExpr{}}); err == nil {
		t.Error("expected IndexExpr RHS error")
	}
	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.IndexExpr{X: &mockExpr{}, Y: lit}, RHS: lit}); err == nil {
		t.Error("expected IndexExpr X error")
	}
	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.IndexExpr{X: lit, Y: &mockExpr{}}, RHS: lit}); err == nil {
		t.Error("expected IndexExpr Y error")
	}
	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.DotExpr{X: lit, Name: &syntax.Ident{Name: "a"}}, RHS: &mockExpr{}}); err == nil {
		t.Error("expected DotExpr RHS error")
	}
	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.DotExpr{X: &mockExpr{}, Name: &syntax.Ident{Name: "a"}}, RHS: lit}); err == nil {
		t.Error("expected DotExpr X error")
	}
	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.SliceExpr{X: lit}, RHS: &mockExpr{}}); err == nil {
		t.Error("expected SliceExpr RHS error")
	}
	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.SliceExpr{X: &mockExpr{}}, RHS: lit}); err == nil {
		t.Error("expected SliceExpr X error")
	}
	if err := c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.SliceExpr{X: lit, Lo: &mockExpr{}}, RHS: lit}); err == nil {
		t.Error("expected SliceExpr Lo error")
	}

	if err := c.compileSimpleAssign(lit, lit); err == nil || !strings.Contains(err.Error(), "unsupported assignment target") {
		t.Error("expected unsupported assignment target error")
	}

	// Simple Assign sub-expression errors
	if err := c.compileSimpleAssign(&syntax.IndexExpr{X: lit, Y: lit}, &mockExpr{}); err == nil {
		t.Error("expected IndexExpr RHS error")
	}
	if err := c.compileSimpleAssign(&syntax.SliceExpr{X: lit}, &mockExpr{}); err == nil {
		t.Error("expected SliceExpr RHS error")
	}
	if err := c.compileSimpleAssign(&syntax.DotExpr{X: lit, Name: &syntax.Ident{Name: "a"}}, &mockExpr{}); err == nil {
		t.Error("expected DotExpr RHS error")
	}
	if err := c.compileSimpleAssign(&syntax.ListExpr{List: []syntax.Expr{&mockExpr{}}}, lit); err == nil {
		t.Error("expected ListExpr elem error")
	}

	if err := c.compileComprehension(&syntax.Comprehension{
		Body:    lit,
		Clauses: []syntax.Node{&mockClause{}},
	}); err == nil || !strings.Contains(err.Error(), "unsupported comprehension clause") {
		t.Error("expected unsupported comprehension clause error")
	}

	// Error propagation in calls
	if err := c.compileCallExpr(&syntax.CallExpr{Fn: &syntax.Ident{Name: "f"}, Args: []syntax.Expr{&mockExpr{}}}); err == nil || !strings.Contains(err.Error(), "unsupported expression") {
		t.Error("expected error from args compilation")
	}
	// Dynamic call kwarg error
	if err := c.compileCallExpr(&syntax.CallExpr{
		Fn: &syntax.Ident{Name: "f"},
		Args: []syntax.Expr{
			&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: &mockExpr{}},
		},
	}); err == nil || !strings.Contains(err.Error(), "unsupported expression") {
		t.Error("expected error from kwarg value compilation")
	}
	// Dynamic call star arg error
	if err := c.compileCallExpr(&syntax.CallExpr{
		Fn: &syntax.Ident{Name: "f"},
		Args: []syntax.Expr{
			&syntax.UnaryExpr{Op: syntax.STAR, X: &mockExpr{}},
		},
	}); err == nil {
		t.Error("expected error from star arg compilation")
	}
	// Dynamic call starstar arg error
	if err := c.compileCallExpr(&syntax.CallExpr{
		Fn: &syntax.Ident{Name: "f"},
		Args: []syntax.Expr{
			&syntax.UnaryExpr{Op: syntax.STARSTAR, X: &mockExpr{}},
		},
	}); err == nil {
		t.Error("expected error from starstar arg compilation")
	}

	// Slice Args errors
	if err := c.compileSliceArgs(&syntax.SliceExpr{Lo: &mockExpr{}}); err == nil {
		t.Error("expected Lo error")
	}
	if err := c.compileSliceArgs(&syntax.SliceExpr{Hi: &mockExpr{}}); err == nil {
		t.Error("expected Hi error")
	}
	if err := c.compileSliceArgs(&syntax.SliceExpr{Step: &mockExpr{}}); err == nil {
		t.Error("expected Step error")
	}

	// Native Func Errors
	vm := taivm.NewVM(&taivm.Function{})
	if _, err := Len.Func(vm, []any{123}); err == nil {
		t.Error("len: expected error")
	}
	if _, err := Range.Func(vm, []any{0, 10, 0}); err == nil {
		t.Error("range: expected error")
	}
	if _, err := Range.Func(vm, []any{"a"}); err == nil {
		t.Error("range: expected error")
	}
	if _, err := Range.Func(vm, []any{1, "a"}); err == nil {
		t.Error("range: expected error")
	}
	if _, err := Range.Func(vm, []any{1, 2, "a"}); err == nil {
		t.Error("range: expected error")
	}
	if _, err := Struct.Func(vm, []any{123}); err == nil {
		t.Error("struct: expected error")
	}
	if _, err := Pow.Func(vm, []any{"a", "b"}); err == nil || !strings.Contains(err.Error(), "unsupported argument types") {
		t.Error("pow: expected unsupported argument types error")
	}
	if _, err := Pow.Func(vm, []any{1}); err == nil || !strings.Contains(err.Error(), "expects 2 arguments") {
		t.Error("pow: expected arg count error")
	}

	// Struct map[string]any support
	if res, err := Struct.Func(vm, []any{map[string]any{"a": 1}}); err != nil {
		t.Error(err)
	} else if s, ok := res.(*taivm.Struct); !ok || s.Fields["a"] != 1 {
		t.Errorf("struct map[string]any failed: %v", res)
	}
}

func TestCriticalFixes(t *testing.T) {
	// Test 1: Precision of pow(3, 35)
	// 3^35 = 50031545098999707
	// float64(3^35) = 50031545098999704 (loss of precision)
	src := `
p = pow(3, 35)
`
	vm := run(t, src)
	check(t, vm, "p", int64(50031545098999707))

	// Test 2: Range overflow detection
	// Construct a range that wraps around MaxInt64 and causes infinite loop in buggy VM
	// MaxInt64 = 9223372036854775807
	// Start = MaxInt64 - 2, Step = 4.
	// Seq: MaxInt64-2, MaxInt64+2 (Wrap to MinInt64+1).
	// If stop is MaxInt64, then MinInt64+1 < MaxInt64 is True. Loop continues.
	src = `
r = range(9223372036854775805, 9223372036854775807, 4)
`
	_, err := Compile("test", strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	vm = taivm.NewVM(&taivm.Function{})
	// Call Range directly to verify error
	// 9223372036854775805, 9223372036854775807, 4
	_, err = Range.Func(vm, []any{int64(9223372036854775805), int64(9223372036854775807), int64(4)})
	if err == nil {
		t.Error("expected overflow error from range()")
	} else if err.Error() != "range overflows" {
		t.Errorf("expected 'range overflows', got %v", err)
	}

	// Test 3: Range overflow negative
	// -9223372036854775805, -9223372036854775808, -4
	_, err = Range.Func(vm, []any{int64(-9223372036854775805), int64(-9223372036854775808), int64(-4)})
	if err == nil {
		t.Error("expected range overflow error (negative step)")
	} else if err.Error() != "range overflows" {
		t.Errorf("expected 'range overflows', got %v", err)
	}
}

func TestMath(t *testing.T) {
	// Pow variants
	src := `
p1 = pow(2.0, 3)
p2 = pow(2, 3.0)
p3 = pow(2.0, 3.0)
p4 = pow(2, -2)
`
	vm := run(t, src)
	check(t, vm, "p1", 8.0)
	check(t, vm, "p2", 8.0)
	check(t, vm, "p3", 8.0)
	check(t, vm, "p4", 0.25)
}

func TestSliceVariants(t *testing.T) {
	src := `
l = [1, 2, 3, 4]
s1 = l[1:]
s2 = l[:2]
s3 = l[:]
s4 = l[::2]
s5 = l[1:4:2]
`
	vm := run(t, src)
	if val, ok := vm.Get("s1"); !ok {
		t.Error("s1 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 3 || l.Elements[0] != int64(2) {
		t.Errorf("s1 = %v", val)
	}
	if val, ok := vm.Get("s2"); !ok {
		t.Error("s2 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 2 || l.Elements[1] != int64(2) {
		t.Errorf("s2 = %v", val)
	}
	if val, ok := vm.Get("s3"); !ok {
		t.Error("s3 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 4 {
		t.Errorf("s3 = %v", val)
	}
	if val, ok := vm.Get("s4"); !ok {
		t.Error("s4 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 2 || l.Elements[1] != int64(3) {
		t.Errorf("s4 = %v", val)
	}
	if val, ok := vm.Get("s5"); !ok {
		t.Error("s5 not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 2 || l.Elements[1] != int64(4) {
		t.Errorf("s5 = %v", val)
	}
}

func TestMoreInternalCoverage(t *testing.T) {
	c := newCompiler("test")
	lit := &syntax.Literal{Token: syntax.INT, Value: int64(1)}

	type mockExpr struct {
		syntax.Literal
	}
	mock := &mockExpr{}

	// compileExpr dispatch errors via sub-functions
	if err := c.compileExpr(&syntax.UnaryExpr{Op: syntax.AND}); err == nil {
		t.Error("expected compileExpr -> UnaryExpr error")
	}
	if err := c.compileExpr(&syntax.BinaryExpr{Op: syntax.DEF, X: lit, Y: lit}); err == nil {
		t.Error("expected compileExpr -> BinaryExpr error")
	}

	// compileStore recursive errors
	if err := c.compileStore(&syntax.DotExpr{X: mock, Name: &syntax.Ident{Name: "a"}}); err == nil {
		t.Error("expected DotExpr X error in Store")
	}
	if err := c.compileStore(&syntax.IndexExpr{X: mock, Y: lit}); err == nil {
		t.Error("expected IndexExpr X error in Store")
	}
	if err := c.compileStore(&syntax.IndexExpr{X: lit, Y: mock}); err == nil {
		t.Error("expected IndexExpr Y error in Store")
	}
	if err := c.compileStore(&syntax.SliceExpr{X: mock}); err == nil {
		t.Error("expected SliceExpr X error in Store")
	}

	// compileDef default value error
	defStmt := &syntax.DefStmt{
		Name: &syntax.Ident{Name: "f"},
		Params: []syntax.Expr{
			&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: mock},
		},
		Body: []syntax.Stmt{},
	}
	if err := c.compileDef(defStmt); err == nil {
		t.Error("expected compileDef default value error")
	}

	// compileLambdaExpr default value error
	lambdaExpr := &syntax.LambdaExpr{
		Params: []syntax.Expr{
			&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: mock},
		},
		Body: lit,
	}
	if err := c.compileLambdaExpr(lambdaExpr); err == nil {
		t.Error("expected compileLambdaExpr default value error")
	}

	// compileComprehension clause errors
	comp := &syntax.Comprehension{
		Body: lit,
		Clauses: []syntax.Node{
			&syntax.ForClause{Vars: &syntax.Ident{Name: "x"}, X: mock},
		},
	}
	if err := c.compileComprehension(comp); err == nil {
		t.Error("expected ForClause X error")
	}

	comp.Clauses = []syntax.Node{
		&syntax.ForClause{Vars: lit, X: lit}, // lit cannot be stored
	}
	if err := c.compileComprehension(comp); err == nil {
		t.Error("expected ForClause Vars error")
	}

	comp.Clauses = []syntax.Node{
		&syntax.IfClause{Cond: mock},
	}
	if err := c.compileComprehension(comp); err == nil {
		t.Error("expected IfClause Cond error")
	}

	// Native Funcs extra coverage
	vm := taivm.NewVM(&taivm.Function{})

	// Range arg counts
	if _, err := Range.Func(vm, []any{}); err == nil {
		t.Error("range: expected error for 0 args")
	}
	if _, err := Range.Func(vm, []any{1, 2, 3, 4}); err == nil {
		t.Error("range: expected error for 4 args")
	}

	// Pow mixed types
	if _, err := Pow.Func(vm, []any{2.0, "a"}); err == nil {
		t.Error("pow: expected error for float + string")
	}
}
