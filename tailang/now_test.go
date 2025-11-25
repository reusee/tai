package tailang

import (
	"strings"
	"testing"
)

func TestNow(t *testing.T) {
	env := NewEnv()

	run := func(src string) {
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("error evaluating %q: %v", src, err)
		}
		if str, ok := res.(string); !ok || str == "" {
			t.Fatalf("expected string result for %q, got %v", src, res)
		}
		t.Logf("%v\n", res)
	}

	run("now")
	run(`now .in "Asia/Shanghai"`)
	run(`now .format "2006-01-02 15:04:05"`)
	run(`now .format "01-02 15:04" .in "Asia/Tokyo"`)
}
