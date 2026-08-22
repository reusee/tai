package gotools

import "testing"

func TestDebugHandleReturnsPointer(t *testing.T) {
	f := Debug(false)
	newDef, remainArgs, err := f.Handle("-debug-gotools", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(remainArgs) != 0 {
		t.Fatalf("expected no remaining args, got %v", remainArgs)
	}
	ret, ok := newDef.(*Debug)
	if !ok {
		t.Fatalf("expected *Debug, got %T", newDef)
	}
	if !bool(*ret) {
		t.Fatal("expected Debug(true)")
	}
}
