package tailang

import (
	"strings"
	"testing"
)

func TestDef(t *testing.T) {
	env := NewEnv()
	src := `
		def foo 42
		def bar "baz"
		+ foo bar
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "42baz" {
		t.Fatalf("got %v", res)
	}

	// typed
	src = `
		def .type int8 i 42
		i
	`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.(int8); !ok {
		t.Fatalf("expected int8, got %T", res)
	}

	// type mismatch
	src = `
		def .type int s "foo"
	`
	tokenizer = NewTokenizer(strings.NewReader(src))
	_, err = env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error")
	}
}
