package tailang

import (
	"strings"
	"testing"
)

func TestEvaluate(t *testing.T) {
	env := NewEnv()
	src := `printf "Hello, %s! Today is %s\n" "world" now .format "01-02" end`
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
}
