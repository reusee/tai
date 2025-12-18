package tailang

import (
	"strings"
	"testing"
)

func BenchmarkEvaluate(b *testing.B) {
	env := NewEnv()
	env.Define("fn", GoFunc{
		Name: "fn",
		Func: func() {},
	})
	for b.Loop() {
		_, err := env.Evaluate(NewTokenizer(strings.NewReader(`fn`)))
		if err != nil {
			b.Fatal(err)
		}
	}
}
