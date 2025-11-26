package tailang

import (
	"strings"
	"testing"
)

func TestType(t *testing.T) {
	env := NewEnv()
	run := func(src string, expected string) {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}
		if res != expected {
			t.Fatalf("src: %s, expected: %s, got: %v", src, expected, res)
		}
	}

	run("type 1", "int")
	run("type 1.5", "float64")
	run(`type "foo"`, "string")
	run("type [ ]", "list")
	run("type &+", "function")
	run("type &def", "function")
	run(`type strings.contains "foo" "f"`, "bool")
}
