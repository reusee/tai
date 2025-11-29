package tailang

import (
	"strings"
	"testing"
)

func TestTypedList(t *testing.T) {
	env := NewEnv()
	runExpectError := func(name, src, errorSnippet string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			tokenizer := NewTokenizer(strings.NewReader(src))
			_, err := env.Evaluate(tokenizer)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), errorSnippet) {
				t.Fatalf("expected error containing %q, got %v", errorSnippet, err)
			}
		})
	}

	// Case 1: Typed list with incompatible element type
	// Previously panicked in convertType or reflect.Append
	runExpectError("ListTypeMismatch", `
		def l [.elem int "string"]
	`, "cannot assign")

	// Case 2: Typed list with nil element (e.g. from if false)
	// Previously panicked in reflect.ValueOf(nil) -> invalid Value
	runExpectError("ListNilElement", `
		def l [.elem int (if false { 1 })]
	`, "cannot assign nil")

}

func TestBugSliceConversion(t *testing.T) {
	env := NewEnv()

	// This should fail before the fix because 'l' is []any and strings.join expects []string
	src := `
		def l ["a" "b"]
		strings.join l ","
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if res != "a,b" {
		t.Fatalf("expected 'a,b', got '%v'", res)
	}
}

func TestBugSliceConversionNested(t *testing.T) {
	env := NewEnv()

	// Register a helper to check [][]int
	env.Define("sum_matrix", GoFunc{
		Name: "sum_matrix",
		Func: func(matrix [][]int) int {
			sum := 0
			for _, row := range matrix {
				for _, v := range row {
					sum += v
				}
			}
			return sum
		},
	})

	src := `
		def m [ [1 2] [3 4] ]
		sum_matrix m
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if res != 10 {
		t.Fatalf("expected 10, got '%v'", res)
	}
}
