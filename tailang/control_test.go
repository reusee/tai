package tailang

import (
	"strings"
	"testing"
)

func TestControl(t *testing.T) {
	env := NewEnv()
	run := func(src string) any {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}
		return res
	}

	// while
	run(`
                def i 0
                def sum 0
                while < i 5 {
                        set sum (+ sum i)
                        set i (+ i 1)
                }
        `)
	if res, _ := env.Lookup("sum"); res != 10 {
		t.Fatalf("while expected sum 10, got %v", res)
	}

	// repeat
	run(`
                def c 0
                repeat i 5 {
                        set c (+ c i)
                }
        `)
	if res, _ := env.Lookup("c"); res != 15 {
		t.Fatalf("repeat expected c 15, got %v", res)
	}

	// foreach
	run(`
                def s ""
                foreach x ["a" "b" "c"] {
                        set s (fmt.sprintf "%s%s" s x end)
                }
        `)
	if res, _ := env.Lookup("s"); res != "abc" {
		t.Fatalf("foreach expected abc, got %v", res)
	}

	// switch
	if res := run(`switch 2 { 1 { "one" } 2 { "two" } }`); res != "two" {
		t.Fatalf("switch expected two, got %v", res)
	}
	if res := run(`switch 3 { 1 { "one" } 2 { "two" } }`); res != nil {
		t.Fatalf("switch expected nil, got %v", res)
	}
	if res := run(`switch 2 { (+ 1 1) { "math" } }`); res != "math" {
		t.Fatalf("switch expected math, got %v", res)
	}
}
