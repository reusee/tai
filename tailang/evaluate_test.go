package tailang

import (
	"reflect"
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

func TestScoping(t *testing.T) {
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

	// 1. def shadowing in block
	run(`
		def x 10
		do {
			def x 20
			if != x 20 { error "inner x should be 20" }
		}
	`)
	if res, _ := env.Lookup("x"); res != 10 {
		t.Fatalf("outer x should remain 10, got %v", res)
	}

	// 2. set modifies outer
	run(`
		def y 10
		do {
			set y 20
		}
	`)
	if res, _ := env.Lookup("y"); res != 20 {
		t.Fatalf("outer y should be 20, got %v", res)
	}

	// 3. func scope is isolated for def
	run(`
		def z 10
		func f() {
			def z 30
		}
		f
	`)
	if res, _ := env.Lookup("z"); res != 10 {
		t.Fatalf("outer z should remain 10, got %v", res)
	}
}

type TestCommand struct {
	Val int `tai:"val"`
}

func (c *TestCommand) FunctionName() string {
	return "cmd"
}

func (c *TestCommand) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	return c.Val, nil
}

func TestStructCommand(t *testing.T) {
	env := NewEnv()
	// We define a struct VALUE. evalCall will make a pointer to a copy of it to set fields.
	env.Define("cmd", TestCommand{Val: 1})

	tokenizer := NewTokenizer(strings.NewReader(`cmd .val 42`))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Fatalf("expected 42, got %v", res)
	}
}

func TestPrefixNotation(t *testing.T) {
	env := NewEnv()
	// (+ 2 (* 3 4)) = 2 + 12 = 14
	res, err := env.Evaluate(NewTokenizer(strings.NewReader(`+ 2 (* 3 4)`)))
	if err != nil {
		t.Fatal(err)
	}
	if res != 14 {
		t.Fatalf("expected 14, got %v", res)
	}
}

func TestPipeLast(t *testing.T) {
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

	// Simple subtraction
	// 2 - 1 = 1
	// 1 |> - 2  => 2 - 1 = 1
	if res := run("1 |> - 2"); res != 1 {
		t.Fatalf("expected 1, got %v", res)
	}
	// 1 | - 2 => 1 - 2 = -1 (Pipe first)
	if res := run("1 | - 2"); res != -1 {
		t.Fatalf("expected -1, got %v", res)
	}

	// User Func
	run(`
		func sub(a b) {
			- a b
		}
	`)
	// sub(2, 1) = 1
	if res := run("1 |> sub 2"); res != 1 {
		t.Fatalf("user func pipe last expected 1, got %v", res)
	}

	// Variadic: strings.join(elems []string, sep string)
	// strings.join takes 2 args, but one is slice.
	// Actually strings.join is func(elems []string, sep string) string. Not variadic in Go's sense?
	// strings.join is defined as func(elems []string, sep string).
	// It is NOT variadic.
	// So [a b] |> strings.join "," should trigger pipeLast on 2nd arg (sep)?
	// No, [a b] is list. "," is string.
	// If I pipe [a b] to strings.join ","
	// i=0: elems. pipeLast=true. i!=numIn-1 (0!=1). Parse from stream -> ",". Type mismatch?
	// strings.join expects []string as first arg.
	// So: "," |> strings.join ["a" "b"]
	// i=0: elems. pipeLast=true. Parse from stream -> ["a" "b"]. Matches.
	// i=1: sep. pipeLast=true. Use pipedVal -> ",".
	if res := run(` "," |> strings.join ["a" "b"] `); res != "a,b" {
		t.Fatalf("stdlib fixed pipe last expected a,b, got %v", res)
	}

	// Variadic GoFunc
	env.Define("concat_all", GoFunc{
		Name: "concat_all",
		Func: func(args ...string) string {
			return strings.Join(args, "")
		},
	})
	// "c" |> concat_all "a" "b" => concat_all("a", "b", "c") => "abc"
	if res := run(` "c" |> concat_all "a" "b" end `); res != "abc" {
		t.Fatalf("variadic pipe last expected abc, got %v", res)
	}
}
