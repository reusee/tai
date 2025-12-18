package tailang

import (
	"strings"
	"testing"
)

func TestNestedLoops(t *testing.T) {
	env := NewEnv()
	src := `
		def res ""
		foreach i ["a" "b"] {
			foreach j ["1" "2"] {
				set res (fmt.sprintf "%s%s%s" [res i j])
			}
		}
		res
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "a1a2b1b2" {
		t.Fatalf("expected a1a2b1b2, got %v", res)
	}
}
