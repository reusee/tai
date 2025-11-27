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
