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
                        set s (fmt.sprintf "%s%s" [s x])
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
	if res := run(`switch 1 { 1 2 3 { "ok" } }`); res != "ok" {
		t.Fatalf("switch expected ok, got %v", res)
	}
	if res := run(`switch 4 { 1 2 3 { "ok" } default { "default" } }`); res != "default" {
		t.Fatalf("switch expected default, got %v", res)
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

func TestControlFlowState(t *testing.T) {
	env := NewEnv()
	src := `
		def i 0
		def acc 0
		while < i 5 {
			if == (% i 2) 0 {
				set acc (+ acc i)
			}
			set i (+ i 1)
		}
		acc
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	// 0 + 2 + 4 = 6
	if res != 6 {
		t.Fatalf("expected 6, got %v", res)
	}
}

func TestConcurrency(t *testing.T) {
	env := NewEnv()
	// Test go, make chan, send, recv
	src := `
		def c (make (chan_of both_dir int) [])
		go {
			send c 42
		}
		recv c
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Errorf("expected 42, got %v", res)
	}

	// Test select
	src = `
		def c1 (make (chan_of both_dir int) [])
		def c2 (make (chan_of both_dir int) 1)
		
		# Send to c2 (buffered)
		select {
			case send c2 100 {
				"sent"
			}
			default {
				"default"
			}
		}
	`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "sent" {
		t.Errorf("expected sent, got %v", res)
	}

	// Recv from c2
	src = `
		select {
			case recv v c2 {
				v
			}
			default {
				0
			}
		}
	`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 100 {
		t.Errorf("expected 100, got %v", res)
	}
}
