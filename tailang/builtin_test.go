package tailang

import (
	"strings"
	"testing"
)

func TestLen(t *testing.T) {
	res, err := NewEnv().Evaluate(NewTokenizer(strings.NewReader(`len "foo"`)))
	if err != nil {
		t.Fatal(err)
	}
	if res != 3 {
		t.Fatalf("got %v", res)
	}
}

func TestMap(t *testing.T) {
	env := NewEnv()
	src := `
		def m (make (map_of string int) [])
		set_index m "foo" 42
		set_index m "bar" 100
		delete m "bar"
		m
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}

	// Convert result to map for checking
	// tailang map is reflected map, so it returns map[string]int (as interface{})
	m, ok := res.(map[string]int)
	if !ok {
		t.Fatalf("expected map[string]int, got %T", res)
	}
	if len(m) != 1 {
		t.Errorf("expected len 1, got %d", len(m))
	}
	if m["foo"] != 42 {
		t.Errorf("expected m[foo]=42, got %v", m["foo"])
	}
	if _, ok := m["bar"]; ok {
		t.Errorf("expected bar to be deleted")
	}

	// Index builtin
	src = `index m "foo"`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Errorf("expected 42, got %v", res)
	}

	// Clear
	src = `
		clear m
		len m
	`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 0 {
		t.Errorf("expected 0 after clear, got %v", res)
	}
}

func TestSliceBuiltins(t *testing.T) {
	env := NewEnv()
	// make, append, set_index, slice
	src := `
		def s (make (slice_of int) [2 5])
		set_index s 0 10
		set_index s 1 20
		set s (append s 30)
		s
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := res.([]int)
	if !ok {
		t.Fatalf("expected []int, got %T", res)
	}
	if len(s) != 3 {
		t.Errorf("expected len 3, got %d", len(s))
	}
	if s[0] != 10 || s[1] != 20 || s[2] != 30 {
		t.Errorf("unexpected elements: %v", s)
	}

	// slice 3-arg
	src = `
		slice s [1 3 3]
	`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	sub, ok := res.([]int)
	if !ok {
		t.Fatalf("expected []int, got %T", res)
	}
	if len(sub) != 2 {
		t.Errorf("expected len 2, got %d", len(sub))
	}
	if sub[0] != 20 || sub[1] != 30 {
		t.Errorf("unexpected subslice: %v", sub)
	}
	if cap(sub) != 2 {
		t.Errorf("expected cap 2, got %d", cap(sub))
	}

	// Copy
	src = `
		def dst (make (slice_of int) 2)
		copy dst [.elem int 1 2]
		dst
	`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := res.([]int)
	if !ok {
		t.Fatalf("expected []int, got %T", res)
	}
	if d[0] != 1 || d[1] != 2 {
		t.Errorf("unexpected copy result: %v", d)
	}
}

func TestComplexBuiltins(t *testing.T) {
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

	c := run(`complex 1 2`)
	if cmplx, ok := c.(complex128); !ok || cmplx != 1+2i {
		t.Errorf("expected 1+2i, got %v", c)
	}

	r := run(`real (complex 1 2)`)
	if f, ok := r.(float64); !ok || f != 1 {
		t.Errorf("expected real 1, got %v", r)
	}

	i := run(`imag (complex 1 2)`)
	if f, ok := i.(float64); !ok || f != 2 {
		t.Errorf("expected imag 2, got %v", i)
	}
}

func TestBuiltinPanicSafety(t *testing.T) {
	env := NewEnv()
	runExpectError := func(name, src, errorSnippet string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			tokenizer := NewTokenizer(strings.NewReader(src))
			// We wrap in a recover to ensure the test fails gracefully if the fix isn't working
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic recovered: %v", r)
				}
			}()

			_, err := env.Evaluate(tokenizer)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), errorSnippet) {
				t.Fatalf("expected error containing %q, got %v", errorSnippet, err)
			}
		})
	}

	// 1. Make with negative size
	runExpectError("MakeNegativeLen", `make (slice_of int) -1`, "negative")
	runExpectError("MakeNegativeCap", `make (slice_of int) [0 -1]`, "negative")
	runExpectError("MakeMapNegative", `make (map_of int int) -5`, "negative")

	// 2. Slice bounds
	runExpectError("SliceStringHighOutOfBounds", `slice "foo" [0 4]`, "out of bounds")
	runExpectError("SliceNegativeLow", `slice "foo" [-1 2]`, "out of bounds")
	runExpectError("SliceLowGtHigh", `slice "foo" [2 1]`, "out of bounds")

	// 3. Slice3 on string
	runExpectError("Slice3String", `slice "foo" [0 1 2]`, "does not support 3-index slicing")

	// 4. Slice bounds on list
	runExpectError("SliceListOutOfBounds", `slice [1 2] [0 5]`, "out of bounds")
	runExpectError("SliceList3OutOfBounds", `slice [1 2] [0 1 5]`, "out of bounds")
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
			case recv c2 v {
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
