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
}
