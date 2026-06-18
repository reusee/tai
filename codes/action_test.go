package codes

import (
	"strings"
	"testing"

	"github.com/reusee/tai/taivm"
)

func TestExpandGoExprs(t *testing.T) {
	env := &taivm.Env{}
	env.Def("foo", "bar")
	env.Def("val", 42)

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    `hello \go("world")`,
			expected: `hello world`,
		},
		{
			input:    `val is \go(val)`,
			expected: `val is 42`,
		},
		{
			input:    `add \go(val + 1)`,
			expected: `add 43`,
		},
		{
			input:    `complex strings \go("paren ) in string") and \go(foo)`,
			expected: `complex strings paren ) in string and bar`,
		},
		{
			input:    `unclosed \go(foo`,
			expected: `unclosed \go(foo`,
		},
	}

	for _, tc := range tests {
		got := expandGoExprs(tc.input, env)
		if got != tc.expected {
			t.Errorf("input: %s, got: %s, expected: %s", tc.input, got, tc.expected)
		}
	}
}

func TestParseFirstBoundaryHunk(t *testing.T) {
	// Valid with blank line
	content := "---change abc-def-gh\nop: MODIFY\ntarget: myFunc\nfile-path: /file.go\n\nfunc myFunc() {}\n\n---end abc-def-gh\n"
	h, start, end, ok := parseFirstBoundaryHunk([]byte(content))
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "MODIFY" {
		t.Fatalf("expected MODIFY, got %s", h.Op)
	}
	if h.Target != "myFunc" {
		t.Fatalf("expected myFunc, got %s", h.Target)
	}
	if h.FilePath != "/file.go" {
		t.Fatalf("expected /file.go, got %s", h.FilePath)
	}
	if !strings.Contains(h.Body, "func myFunc() {}") {
		t.Fatal("body does not contain expected code")
	}
	expectedEnd := len(content)
	if end != expectedEnd {
		t.Fatalf("expected end %d, got %d", expectedEnd, end)
	}
	_ = start

	// Body line that looks like a header after all headers are set (no blank line)
	content2 := "---change x-y-z\nop: MODIFY\ntarget: myFunc\nfile-path: /file.go\nop: MODIFY // comment\nfunc myFunc() {}\n\n---end x-y-z\n"
	h2, _, _, ok2 := parseFirstBoundaryHunk([]byte(content2))
	if !ok2 {
		t.Fatal("expected ok for content2")
	}
	if h2.Op != "MODIFY" {
		t.Fatal("op should remain MODIFY, not overwritten")
	}
	if !strings.Contains(h2.Body, "op: MODIFY // comment") {
		t.Fatal("body should contain the header-like line")
	}
}