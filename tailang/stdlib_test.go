package tailang

import (
	"fmt"
	"strings"
	"testing"
)

func TestStdlib(t *testing.T) {
	env := NewEnv()

	type kase struct {
		Source   string
		Expected any
	}
	kases := []kase{
		{
			Source:   `fmt.sprintf '%v %v' "hello" "world"`,
			Expected: "hello world",
		},
		{
			Source:   `strings.fields "foo bar baz"`,
			Expected: `[foo bar baz]`,
		},
		{
			Source:   `strings.join ['foo' 'bar'] ','`,
			Expected: `foo,bar`,
		},
	}

	for _, c := range kases {
		tokenizer := NewTokenizer(strings.NewReader(c.Source))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%v", res) != c.Expected {
			t.Fatalf("in %+v, got %v", c, res)
		}
	}

}
