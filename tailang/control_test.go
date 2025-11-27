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

	// do
	run(`
		def do_res 0
		do {
			set do_res 42
		}
	`)
	if res, _ := env.Lookup("do_res"); res != 42 {
		t.Fatalf("do expected 42, got %v", res)
	}

	run(`
		def blk {
			set do_res 100
		}
		do blk
	`)
	if res, _ := env.Lookup("do_res"); res != 100 {
		t.Fatalf("do block variable expected 100, got %v", res)
	}
}

func TestScopeLeakage(t *testing.T) {
	env := NewEnv()

	// Helper to check that accessing a variable returns an error (undefined)
	assertUndefined := func(src string) {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		_, err := env.Evaluate(tokenizer)
		if err == nil {
			t.Fatalf("expected error for undefined variable in src: %s, but got nil", src)
		}
		if !strings.Contains(err.Error(), "undefined identifier") {
			t.Fatalf("expected undefined identifier error, got: %v", err)
		}
	}

	// if
	assertUndefined(`
		if true {
			def leaked_if 1
		}
		leaked_if
	`)

	// while
	assertUndefined(`
		def i 0
		while < i 1 {
			def leaked_while 1
			set i (+ i 1)
		}
		leaked_while
	`)

	// do
	assertUndefined(`
		do {
			def leaked_do 1
		}
		leaked_do
	`)

	// switch
	assertUndefined(`
		switch 1 {
			1 {
				def leaked_switch 1
			}
		}
		leaked_switch
	`)
}
