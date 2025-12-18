package tailang

import (
	"strings"
	"testing"
)

func TestDefer(t *testing.T) {
	env := NewEnv()
	src := `
		def res ""
		func f() {
			defer { set res (+ res "2") }
			defer { set res (+ res "1") }
			set res "0"
		}
		f
		res
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "012" {
		t.Fatalf("expected 012, got %v", res)
	}
}

func TestDeferScope(t *testing.T) {
	env := NewEnv()
	src := `
		def res 0
		func f() {
			def i 1
			defer { set res i }
			set i 2
		}
		f
		res
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	// Defer block captures reference to i
	if res != 2 {
		t.Fatalf("expected 2, got %v", res)
	}
}

func TestDeferOutsideFunctionError(t *testing.T) {
	env := NewEnv()
	src := `defer { print "should not run" }`
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error when defer outside function")
	}
	if !strings.Contains(err.Error(), "defer must be inside a function") {
		t.Fatalf("expected error about defer inside function, got %v", err)
	}
}
