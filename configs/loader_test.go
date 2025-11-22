package configs

import (
	"errors"
	"fmt"
	"testing"
)

var testSchema = `
str?: string
list?: [...int]
`

func TestLoaderAssignFirst(t *testing.T) {
	loader := NewLoader([]string{"test.cue"}, testSchema)

	var str string
	err := loader.AssignFirst("str", &str)
	if err != nil {
		t.Fatal(err)
	}
	if str != "bar" {
		t.Fatalf("got %q", str)
	}

	var list []int
	err = loader.AssignFirst("list", &list)
	if err != nil {
		t.Fatal(err)
	}
	if str := fmt.Sprintf("%v", list); str != "[1 2 3]" {
		t.Fatalf("got %s", str)
	}

	err = loader.AssignFirst("not", &list)
	if !errors.Is(err, ErrValueNotFound) {
		t.Fatalf("got %v", err)
	}

}

func TestLoaderIterCueValues(t *testing.T) {
	loader := NewLoader([]string{
		"test.cue",
		"test2.cue",
	}, testSchema)

	var strs []string
	for value, err := range loader.IterCueValues("str") {
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if err := value.Decode(&s); err != nil {
			t.Fatal(err)
		}
		strs = append(strs, s)
	}
	if str := fmt.Sprintf("%v", strs); str != "[bar foo]" {
		t.Fatalf("got %q", str)
	}

	strs = strs[:0]
	for str := range All[string](loader, "str") {
		strs = append(strs, str)
	}
	if str := fmt.Sprintf("%v", strs); str != "[bar foo]" {
		t.Fatalf("got %q", str)
	}

}

func TestUnknownField(t *testing.T) {
	loader := NewLoader([]string{
		"bad.cue",
	}, testSchema)
	var str string
	err := loader.AssignFirst("unknown_field", &str)
	if err == nil {
		t.Fatal("should error")
	}
	t.Logf("%v", err)
}
