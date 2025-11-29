package tailang

import (
	"strings"
	"testing"
)

func TestPanicNilVariable(t *testing.T) {
	env := NewEnv()
	// Define a variable that evaluates to nil
	// Using 'if false { 1 }' results in nil
	src := `
		def n (if false { 1 })
		n
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil, got %v", res)
	}
}

func TestPanicNamedParamOnPrimitive(t *testing.T) {
	env := NewEnv()
	src := `
		def i 10
		i .p 5
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "named parameter") {
		t.Fatalf("expected named parameter error, got: %v", err)
	}
}

func TestPanicForeachNil(t *testing.T) {
	env := NewEnv()
	src := `
		def n (if false { 1 })
		foreach x n {
			x
		}
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "expects a list") {
		t.Fatalf("expected expects a list error, got: %v", err)
	}
}

func TestPanicCallNonFunc(t *testing.T) {
	env := NewEnv()
	// Manually define a GoFunc with a non-function value to trigger callFunc.
	// This simulates a misconfigured GoFunc or an internal error where a GoFunc is created with a wrong type.
	env.Define("bad_func", GoFunc{
		Name: "bad",
		Func: 1, // Not a function
	})

	src := `bad_func`
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot call non-function") {
		t.Fatalf("expected cannot call non-function error, got: %v", err)
	}
}

func TestPipe(t *testing.T) {
	env := NewEnv()
	run := func(src string) any {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}
		return res
	}

	// Simple math
	if res := run("1 | + 2"); res != 3 {
		t.Fatalf("expected 3, got %v", res)
	}

	// Chain
	if res := run("1 | + 2 | + 3"); res != 6 {
		t.Fatalf("expected 6, got %v", res)
	}

	// User function
	run(`
		func add(a b) {
			+ a b
		}
	`)
	if res := run("1 | add 2"); res != 3 {
		t.Fatalf("user func pipe expected 3, got %v", res)
	}

	// Stdlib
	if res := run(` "foo" | strings.to_upper `); res != "FOO" {
		t.Fatalf("stdlib pipe expected FOO, got %v", res)
	}

	if res := run(` "  foo  " | strings.trim_space `); res != "foo" {
		t.Fatalf("stdlib pipe expected foo, got %v", res)
	}

	// Variadic: strings.join(elems []string, sep string)
	// pipe passes []string as first arg
	if res := run(` ["a" "b"] | strings.join "," `); res != "a,b" {
		t.Fatalf("stdlib variadic pipe expected a,b, got %v", res)
	}

	// Variadic GoFunc where pipe provides first arg, and remaining args are also provided
	env.Define("sum_all", GoFunc{
		Name: "sum_all",
		Func: func(args ...int) int {
			s := 0
			for _, v := range args {
				s += v
			}
			return s
		},
	})

	// Pipe 1 into sum_all 2 3 -> sum_all(1, 2, 3) -> 6
	if res := run("1 | sum_all 2 3"); res != 6 {
		t.Fatalf("variadic pipe expected 6, got %v", res)
	}
}

func TestPipeErrors(t *testing.T) {
	env := NewEnv()
	runExpectError := func(src, errorSnippet string) {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		_, err := env.Evaluate(tokenizer)
		if err == nil {
			t.Fatalf("expected error for src: %s, got nil", src)
		}
		if !strings.Contains(err.Error(), errorSnippet) {
			t.Fatalf("expected error containing %q, got %v", errorSnippet, err)
		}
	}

	runExpectError("1 | 2", "cannot pipe into number literal")
	runExpectError(`1 | "foo"`, "cannot pipe into string literal")
	runExpectError(`1 | ( + 1 2 )`, "cannot pipe into parenthesized expression")

	// Undefined identifier
	runExpectError("1 | unknown_func", "undefined identifier")

	// Pipe into non-function value (variable)
	runExpectError(`
		def x 1
		1 | x
	`, "cannot pipe into non-callable value")
}
