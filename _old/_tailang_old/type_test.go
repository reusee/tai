package tailang

import (
	"reflect"
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

func TestListElem(t *testing.T) {
	env := NewEnv()

	run := func(src string, expectedType reflect.Type) {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}
		resType := reflect.TypeOf(res)
		if resType != expectedType {
			t.Fatalf("src: %s, expected type: %v, got: %v", src, expectedType, resType)
		}
	}

	run("[ .elem int 1 2 3 ]", reflect.TypeOf([]int{}))
	run("[ .elem float64 1 2 3 ]", reflect.TypeOf([]float64{}))
	run("[ .elem string 'a' 'b' ]", reflect.TypeOf([]string{}))
	run("[ 1 2 3 ]", reflect.TypeOf([]any{}))
}
