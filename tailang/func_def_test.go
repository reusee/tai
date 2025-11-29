package tailang

import (
	"strings"
	"testing"
)

func TestFunc(t *testing.T) {
	env := NewEnv()
	src := `
		func add(a b) {
			+ a b
		}
		add 1 2
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 3 {
		t.Fatalf("got %v", res)
	}
}

func TestFuncScope(t *testing.T) {
	env := NewEnv()
	src := `
		def x "outer"
		func foo() {
			x
		}
		foo
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "outer" {
		t.Fatalf("expected outer, got %v", res)
	}
}

func TestFuncRef(t *testing.T) {
	env := NewEnv()
	src := `
		func foo(x) {
			x
		}
		def f &foo
		f f f f f 42
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Fatalf("expected 42, got %v", res)
	}
}

func TestFuncNested(t *testing.T) {
	env := NewEnv()
	src := `
		func make_adder(x) {
			func adder (y) {
				+ x y
			}
			&adder
		}
		def add1 make_adder 1
		add1 2
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 3 {
		t.Fatalf("got %v", res)
	}
}

func TestFuncAsArg(t *testing.T) {
	env := NewEnv()
	// Define a Go function that takes a function argument
	env.Define("apply", GoFunc{
		Name: "apply",
		Func: func(fn func(int) int, val int) int {
			return fn(val)
		},
	})
	env.Define("apply_err", GoFunc{
		Name: "apply_err",
		Func: func(fn func(int) (int, error), val int) (int, error) {
			return fn(val)
		},
	})

	src := `
		func add1(x) {
			+ x 1
		}
		apply &add1 41
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Fatalf("got %v", res)
	}

	// Test error propagation
	srcErr := `
		func fail(x) {
			fmt.errorf "fail"
		}
		apply_err &fail 1
	`
	tokenizer = NewTokenizer(strings.NewReader(srcErr))
	_, err = env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fail") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFib(t *testing.T) {
	env := NewEnv()
	src := `
		func fib(n) {
			if <= n 1 {
				n
			} else {
				+ (fib (- n 1)) (fib (- n 2))
			}
		}
		fib 10
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 55 {
		t.Fatalf("expected 55, got %v", res)
	}
}

func TestVariadicInParens(t *testing.T) {
	env := NewEnv()
	env.Define("sum", GoFunc{
		Name: "sum",
		Func: func(args ...int) int {
			s := 0
			for _, v := range args {
				s += v
			}
			return s
		},
	})

	// This previously failed because 'sum' would try to consume ')'
	src := `(sum 1 2 3)`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != 6 {
		t.Fatalf("expected 6, got %v", res)
	}

	// Test with nested parens
	src = `(sum 1 (sum 2 3) 4)`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != 10 {
		t.Fatalf("expected 10, got %v", res)
	}
}

func TestClosureCounter(t *testing.T) {
	env := NewEnv()
	src := `
		func make_counter(start) {
			def count start
			func inc() {
				set count (+ count 1)
				count
			}
			&inc
		}
		def c1 make_counter 0
		def c2 make_counter 10
		def r1 c1
		def r2 c1
		def r3 c2
		(fmt.sprintf "%v %v %v" r1 r2 r3)
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "1 2 11" {
		t.Fatalf("expected '1 2 11', got '%v'", res)
	}
}

func TestRecursionFactorial(t *testing.T) {
	env := NewEnv()
	src := `
		func fact(n) {
			if <= n 1 {
				1
			} else {
				* n (fact (- n 1))
			}
		}
		fact 5
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 120 {
		t.Fatalf("expected 120, got %v", res)
	}
}
