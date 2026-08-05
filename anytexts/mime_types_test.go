package anytexts

import "testing"

func TestIncludeMimeTypesHandleReturnsPointer(t *testing.T) {
	f := IncludeMimeTypes{}
	newDef, remainArgs, err := f.Handle("-pdf", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(remainArgs) != 0 {
		t.Fatalf("expected no remaining args, got %v", remainArgs)
	}
	ret, ok := newDef.(*IncludeMimeTypes)
	if !ok {
		t.Fatalf("expected *IncludeMimeTypes, got %T", newDef)
	}
	if !(*ret)["application/pdf"] {
		t.Fatal("expected application/pdf to be included in the returned def")
	}
}
