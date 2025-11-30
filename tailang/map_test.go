package tailang

import (
	"reflect"
	"strings"
	"testing"
)

func TestMapCreation(t *testing.T) {
	env := NewEnv()

	// Create map type map[string]int
	src := `
		def m (make (map_of string int))
		set_index m "foo" 42
		index m "foo"
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Fatalf("expected 42, got %v", res)
	}

	// Verify type
	srcType := `
		def m (make (map_of string int))
		m
	`
	tokenizer = NewTokenizer(strings.NewReader(srcType))
	resMap, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}

	expectedType := reflect.MapOf(reflect.TypeOf(""), reflect.TypeOf(0))
	if reflect.TypeOf(resMap) != expectedType {
		t.Fatalf("expected %v, got %v", expectedType, reflect.TypeOf(resMap))
	}
}
