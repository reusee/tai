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

func TestGoSrcPromptsEndWithSummary(t *testing.T) {
	// The go-src stop rule must be phrased like the shell prompt's: stop
	// generating, end the response with a summary block, and wait. A bare
	// "stop generating and wait" licenses omitting the summary block and
	// contradicts SummaryBlockSystemPrompt's every-response requirement.
	// See TheoryOfGoSrcBlocks and TheoryOfSummaryBlocks.
	if !strings.Contains(GoSrcBlockSystemPrompt, "end the response with a summary block") {
		t.Fatal("system prompt must phrase the stop rule as ending the response with a summary block")
	}
	if strings.Contains(GoSrcBlockSystemPrompt, "stop generating and wait:") {
		t.Fatal("system prompt must not carry a stop instruction that omits the summary block")
	}
}
