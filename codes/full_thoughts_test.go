package codes

import (
	"testing"

	"github.com/reusee/tai/flags"
)

func TestFullThoughtsFlag(t *testing.T) {
	// Verify FullThoughts satisfies the Flag interface.
	var _ flags.Flag = FullThoughts(false)

	// Verify default is false.
	module := Module{}
	if ft := module.FullThoughts(); bool(ft) {
		t.Fatal("default FullThoughts should be false")
	}

	// Verify Keys.
	f := FullThoughts(false)
	keys := f.Keys()
	if _, ok := keys["-full-thoughts"]; !ok {
		t.Fatal("Keys must include -full-thoughts")
	}

	// Verify Handle returns FullThoughts(true) and preserves remaining args.
	newValue, remainArgs, err := f.Handle("-full-thoughts", []string{"arg"})
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	full, ok := newValue.(FullThoughts)
	if !ok {
		t.Fatalf("expected FullThoughts, got %T", newValue)
	}
	if !bool(full) {
		t.Fatal("expected FullThoughts(true) after Handle")
	}
	if len(remainArgs) != 1 || remainArgs[0] != "arg" {
		t.Fatalf("expected remaining args unchanged, got %v", remainArgs)
	}
}
