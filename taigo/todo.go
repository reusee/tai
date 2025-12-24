package taigo

/* TODO fix tests
--- FAIL: TestVM (0.00s)
    --- FAIL: TestVM/loops (0.00s)
        run.go:1429: vm error: math operands must be numeric, got *taivm.Closure and int64
    --- FAIL: TestVM/switch (0.00s)
        vm_test.go:181: variable res2 not found
    --- FAIL: TestVM/functions (0.00s)
        run.go:557: vm error: undefined variable: a
    --- FAIL: TestVM/closures (0.00s)
        run.go:557: vm error: undefined variable: base
    --- FAIL: TestVM/slices (0.00s)
        run.go:957: vm error: index out of bounds: 3
    --- FAIL: TestVM/maps (0.00s)
        vm_test.go:278: variable v not found
    --- FAIL: TestVM/range_map (0.00s)
        run.go:591: vm error: variable not found: k
    --- FAIL: TestVM/variadic_function (0.00s)
        vm_test.go:325: main:3:20: expected type, found ')'
FAIL
exit status 1
FAIL	github.com/reusee/tai/taigo	0.002s
*/
