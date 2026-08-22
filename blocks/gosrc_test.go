package blocks

import (
	"reflect"
	"strings"
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

func TestGoSrcPromptsDescribePackageSymbols(t *testing.T) {
	// The go-src prompts must teach the package form: a symbol that is
	// a loaded package's exact import path or package name returns the
	// package's go doc documentation, with command and unexported
	// documentation for focus packages. See TheoryOfGoSrcBlocks.
	for name, prompt := range map[string]string{
		"GoSrcBlockSystemPrompt":  GoSrcBlockSystemPrompt,
		"GoSrcBlockRestatePrompt": GoSrcBlockRestatePrompt,
	} {
		if !strings.Contains(prompt, "go doc documentation") ||
			!strings.Contains(prompt, "package name") {
			t.Fatalf("%s does not describe package symbols", name)
		}
	}
}
