package tailang

import (
	"strings"
	"testing"
)

func TestStringComparison(t *testing.T) {
	env := NewEnv()
	run := func(src string, expected bool) {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}
		if res != expected {
			t.Fatalf("src: %s, expected: %v, got: %v", src, expected, res)
		}
	}

	run(`< "a" "b"`, true)
	run(`> "b" "a"`, true)
	run(`<= "a" "a"`, true)
	run(`>= "b" "b"`, true)
	run(`< "b" "a"`, false)
}
