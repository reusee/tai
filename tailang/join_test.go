package tailang

import (
	"strings"
	"testing"
)

func TestJoin(t *testing.T) {
	env := NewEnv()

	tests := []struct {
		src string
		exp string
	}{
		{`join "," "a" "b" "c" end`, "a,b,c"},
		{`join "," [ "a" "b" "c" ]`, "a,b,c"},
		{`join "-" 1 2 3 end`, "1-2-3"},
		{`join "" [ "foo" ]`, "foo"},
		{`join "." end`, ""},
		{`join "," [ "a" [ "b" "c" ] ]`, "a,[b c]"},
	}

	for i, test := range tests {
		tokenizer := NewTokenizer(strings.NewReader(test.src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Errorf("case %d: error evaluating %q: %v", i, test.src, err)
			continue
		}
		if res != test.exp {
			t.Errorf("case %d: expected %q, got %q", i, test.exp, res)
		}
	}
}
