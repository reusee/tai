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
