package tailang

import (
	"strings"
	"testing"
)

func TestBlockParsingWithStrings(t *testing.T) {
	env := NewEnv()
	run := func(src string, expected any) {
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

	// String containing closing brace
	run(`
		def s ""
		do {
			set s "}"
		}
		s
	`, "}")

	// String containing opening brace
	run(`
		def s ""
		do {
			set s "{"
		}
		s
	`, "{")

	// Nested blocks with brace strings
	run(`
		def s ""
		do {
			do {
				set s "}{"
			}
		}
		s
	`, "}{")
}
