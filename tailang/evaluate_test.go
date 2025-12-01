package tailang

import (
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestMethod(t *testing.T) {
	env := NewEnv()
	src := `time.now:Format "2006-01-02"`
	res, err := env.Evaluate(NewTokenizer(strings.NewReader(src)))
	if err != nil {
		t.Fatal(err)
	}
	if res != time.Now().Format("2006-01-02") {
		t.Fatalf("got %v", res)
	}
}

func TestMethodReference(t *testing.T) {
	env := NewEnv()
	src := `
		def fmt time.now::Format
		fmt "2006"
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "2025" {
		t.Fatalf("got %v", res)
	}
}

func TestTypeAsArgument(t *testing.T) {
	env := NewEnv()
	env.Define("foo", GoFunc{
		Name: "foo",
		Func: func(t reflect.Type) reflect.Type {
			return reflect.PointerTo(t)
		},
	})
	src := `foo int`
	_, err := env.Evaluate(NewTokenizer(strings.NewReader(src)))
	if err != nil {
		t.Fatal(err)
	}
}

func TestPosErrorFormatting(t *testing.T) {
	env := NewEnv()
	// Source with multiple lines
	src := `
def x 1
call_undefined
`
	// The tokenizer creates a source. The error should point to call_undefined at line 3.
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error")
	}

	// Check formatting
	msg := err.Error()
	if !strings.Contains(msg, "undefined identifier: call_undefined") {
		t.Errorf("unexpected error message: %s", msg)
	}
	if !strings.Contains(msg, "at :3:1") { // empty filename
		t.Errorf("expected location info, got: %s", msg)
	}
	if !strings.Contains(msg, "call_undefined") {
		t.Errorf("expected source line in error, got: %s", msg)
	}
	if !strings.Contains(msg, "^") {
		t.Errorf("expected caret in error, got: %s", msg)
	}
}

func TestTypeConversion(t *testing.T) {
	env := NewEnv()

	// string -> []byte
	src := `
		def s "hello"
		def b (make (slice_of byte) 0)
		set b s
		b
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	bs, ok := res.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", res)
	}
	if string(bs) != "hello" {
		t.Errorf("expected hello bytes, got %s", string(bs))
	}

	// []byte -> string
	src = `
		def s2 ""
		set s2 b
		s2
	`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "hello" {
		t.Errorf("expected hello string, got %v", res)
	}
}
