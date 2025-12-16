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
			Source:   `fmt.sprintf '%v %v' ["hello" "world"]`,
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

func TestRegexp(t *testing.T) {
	env := NewEnv()
	run := func(src string, expected bool) {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatal(err)
		}
		if res != expected {
			t.Errorf("src: %s, expected %v, got %v", src, expected, res)
		}
	}

	run(`regexp.match_string "^f.o$" "foo"`, true)
	run(`regexp.match_string "^f.o$" "bar"`, false)
	run(`regexp.match_string "\\d+" "123"`, true)
}

func TestBase64(t *testing.T) {
	env := NewEnv()
	src := `base64.std_encoding.encode_to_string "hello"`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "aGVsbG8=" {
		t.Errorf("expected aGVsbG8=, got %v", res)
	}

	src = `base64.std_encoding.decode_string "aGVsbG8="`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if string(res.([]byte)) != "hello" {
		t.Errorf("expected hello, got %v", res)
	}
}
