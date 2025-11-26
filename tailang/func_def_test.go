package tailang

import (
	"strings"
	"testing"
)

func TestFunc(t *testing.T) {
	env := NewEnv()
	src := `
		func add [ a b ] [
			join + a b end
		]
		add 1 2
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "1+2" {
		t.Fatalf("expected 1+2, got %v", res)
	}
}

func TestFuncScope(t *testing.T) {
	env := NewEnv()
	src := `
		def x "outer"
		func foo [ ] [
			x
		]
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
		func foo [ x ] [
			x
		]
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
		func make_adder [ x ] [
			func adder [ y ] [
				join + x y end
			]
			&adder
		]
		def add1 make_adder 1
		add1 2
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "1+2" {
		t.Fatalf("expected 1+2, got %v", res)
	}
}
