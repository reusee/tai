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
		{
			Source: `
				def l [ .elem int 1 3 2]
				sort.ints l
				l
			`,
			Expected: "[1 2 3]",
		},
		{
			Source: `
				func map(r) {
					+ r 1
				}
				strings.map &map "foo"
			`,
			Expected: "gpp",
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
