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
