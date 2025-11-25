package tailang

import (
	"strings"
	"testing"
)

func TestEvaluate(t *testing.T) {
	env := NewEnv()
	src := `
		printf "Hello, %s! Today is %s\n"
			"world"
			now .format "01-02" 
		end
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnquotedString(t *testing.T) {
	env := NewEnv()
	src := `
		join , "a" "b" end
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "a,b" {
		t.Fatalf("expected a,b got %v", res)
	}

	src = `
		now .format YYYY-MM-DD
	`
	tokenizer = NewTokenizer(strings.NewReader(src))
	res, err = env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.(string); !ok {
		t.Fatalf("expected string got %v", res)
	}
}
