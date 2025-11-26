package tailang

import (
	"fmt"
	"strings"
	"testing"
)

func TestGoFunc(t *testing.T) {
	env := NewEnv()

	// Simple function
	env.Define("add", GoFunc{
		Name: "add",
		Func: func(a, b int) int {
			return a + b
		},
	})

	// Variadic
	env.Define("sum", GoFunc{
		Name: "sum",
		Func: func(args ...int) int {
			s := 0
			for _, v := range args {
				s += v
			}
			return s
		},
	})

	// Error return
	env.Define("fail", GoFunc{
		Name: "fail",
		Func: func() (int, error) {
			return 0, fmt.Errorf("failed")
		},
	})

	// With Env
	env.Define("get_val", GoFunc{
		Name: "get_val",
		Func: func(e *Env, name string) any {
			v, _ := e.Lookup(name)
			return v
		},
	})

	run := func(src string) (any, error) {
		tokenizer := NewTokenizer(strings.NewReader(src))
		return env.Evaluate(tokenizer)
	}

	res, err := run("add 1 2")
	if err != nil {
		t.Fatal(err)
	}
	if res != 3 {
		t.Fatalf("expected 3, got %v", res)
	}

	res, err = run("sum 1 2 3 4 end")
	if err != nil {
		t.Fatal(err)
	}
	if res != 10 {
		t.Fatalf("expected 10, got %v", res)
	}

	_, err = run("fail")
	if err == nil {
		t.Fatal("expected error")
	}

	env.Define("foo", 42)
	res, err = run(`get_val "foo"`)
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Fatalf("expected 42, got %v", res)
	}
}
