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
		join , foo bar end
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "42,baz" {
		t.Fatalf("expected 42-baz, got %v", res)
	}
}
