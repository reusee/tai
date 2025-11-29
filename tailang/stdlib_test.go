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

func TestJSON(t *testing.T) {
	env := NewEnv()
	// Unmarshal
	src := `json.unmarshal '{"foo": "bar", "baz": 123}'`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", res)
	}
	if m["foo"] != "bar" {
		t.Errorf("expected bar, got %v", m["foo"])
	}
	if v, ok := m["baz"].(float64); !ok || v != 123 {
		t.Errorf("expected 123, got %v (%T)", m["baz"], m["baz"])
	}

	// Marshal
	src = `json.marshal (json.unmarshal '["a", "b"]')`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	bytes, ok := res.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", res)
	}
	if string(bytes) != `["a","b"]` {
		t.Errorf("expected [\"a\",\"b\"], got %s", string(bytes))
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
