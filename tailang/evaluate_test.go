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
