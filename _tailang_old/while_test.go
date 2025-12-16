package tailang

import (
	"strings"
	"testing"
)

func TestControlFlowState(t *testing.T) {
	env := NewEnv()
	src := `
		def i 0
		def acc 0
		while < i 5 {
			if == (% i 2) 0 {
				set acc (+ acc i)
			}
			set i (+ i 1)
		}
		acc
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	// 0 + 2 + 4 = 6
	if res != 6 {
		t.Fatalf("expected 6, got %v", res)
	}
}
