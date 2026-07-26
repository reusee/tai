package generators

import (
	"testing"
)

func TestDeepseekTokenCounter(t *testing.T) {
	counter := DeepseekTokenCounterFn

	testCases := []struct {
		input  string
		expect int
	}{
		{"", 0},
		{"hello", 1},    // 5 * 0.3 = 1.5 → int 1
		{"世界", 1},       // 2 * 0.6 = 1.2 → int 1
		{"hello世界", 2},  // 1.5 + 1.2 = 2.7 → int 2
		{"hello 世界", 3}, // 6 * 0.3 + 2 * 0.6 = 3.0 → int 3
	}

	for _, tc := range testCases {
		got, err := counter(tc.input)
		if err != nil {
			t.Errorf("unexpected error for %q: %v", tc.input, err)
		}
		if got != tc.expect {
			t.Errorf("for %q: got %d, expect %d", tc.input, got, tc.expect)
		}
	}
}
