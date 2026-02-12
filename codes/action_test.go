package codes

import (
	"github.com/reusee/tai/taivm"
	"testing"
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