package blocks

import (
	"reflect"
	"testing"
)

func TestParseGoSrcSymbols(t *testing.T) {
	blocks := []Block{
		{Kind: "summary", Body: "- done"},
		{Kind: "go-src", Body: "Foo\n\n  Bar.Read  \n*Baz.Write"},
		{Kind: "go-src", Body: "   "},
	}
	got := ParseGoSrcSymbols(blocks)
	want := []string{"Foo", "Bar.Read", "*Baz.Write"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseGoSrcSymbols = %v, want %v", got, want)
	}
	if got := ParseGoSrcSymbols(nil); got != nil {
		t.Fatalf("expected nil for no blocks, got %v", got)
	}
	if got := ParseGoSrcSymbols([]Block{{Kind: "shell", Body: "ls"}}); got != nil {
		t.Fatalf("expected nil for non-go-src blocks, got %v", got)
	}
}
