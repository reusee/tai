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
