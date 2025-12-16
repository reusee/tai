package tailang

import (
	"strings"
	"testing"
)

func TestSwitchDefaultOrder(t *testing.T) {
	env := NewEnv()
	src := `switch 1 { default { "default" } 1 { "one" } }`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "one" {
		t.Fatalf("expected 'one', got %v", res)
	}
}

func TestSwitchDefaultFallback(t *testing.T) {
	env := NewEnv()
	src := `switch 2 { 1 { "one" } default { "default" } }`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "default" {
		t.Fatalf("expected 'default', got %v", res)
	}
}

func TestSwitchDuplicateDefaultError(t *testing.T) {
	env := NewEnv()
	src := `switch 1 { default { } default { } }`
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error for duplicate default")
	}
	if !strings.Contains(err.Error(), "multiple default clauses") {
		t.Fatalf("expected duplicate default error, got %v", err)
	}
}

func TestSwitchDanglingValuesError(t *testing.T) {
	env := NewEnv()
	src := `switch 1 { 1 2 }`
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error for missing block")
	}
	if !strings.Contains(err.Error(), "without a block") {
		t.Fatalf("expected missing block error, got %v", err)
	}
}

func TestSwitchBlockWithoutValuesError(t *testing.T) {
	env := NewEnv()
	src := `switch 1 { { "block" } }`
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err == nil {
		t.Fatal("expected error for block without values")
	}
	if !strings.Contains(err.Error(), "block without preceding case values") {
		t.Fatalf("expected block without values error, got %v", err)
	}
}
