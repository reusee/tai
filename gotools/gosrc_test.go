package gotools

import (
	"reflect"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
)

func TestParseGoSrcSymbols(t *testing.T) {
	bs := []blocks.Block{
		{Kind: "summary", Body: "- done"},
		{Kind: "go-src", Body: "Foo\n\n  Bar.Read  \n*Baz.Write"},
		{Kind: "go-src", Body: "   "},
	}
	got := ParseGoSrcSymbols(bs)
	want := []string{"Foo", "Bar.Read", "*Baz.Write"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseGoSrcSymbols = %v, want %v", got, want)
	}
	if got := ParseGoSrcSymbols(nil); got != nil {
		t.Fatalf("expected nil for no blocks, got %v", got)
	}
	if got := ParseGoSrcSymbols([]blocks.Block{{Kind: "shell", Body: "ls"}}); got != nil {
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

func TestGoSrcPromptsPreferGoSrcOverRead(t *testing.T) {
	// The go-src prompts must teach the division of labor with read:
	// Go source is fetched by symbol — gaining the defining file and the
	// references report — while read serves non-Go files, whole-file
	// views, glob discovery, and network resources. See
	// TheoryOfGoSrcBlocks.
	for name, prompt := range map[string]string{
		"GoSrcBlockSystemPrompt":  GoSrcBlockSystemPrompt,
		"GoSrcBlockRestatePrompt": GoSrcBlockRestatePrompt,
	} {
		if !strings.Contains(prompt, "Prefer go-src over read") {
			t.Fatalf("%s does not teach the go-src preference for Go source", name)
		}
		if !strings.Contains(prompt, "references report") {
			t.Fatalf("%s does not cite the references report as the reason for the preference", name)
		}
		if !strings.Contains(prompt, "non-Go files") {
			t.Fatalf("%s does not delineate the read block's remaining uses", name)
		}
	}
}

func TestGoSrcPromptsDescribeSnapshotAndFilePath(t *testing.T) {
	// The go-src prompts must teach three facts about resolution results:
	// prefer the import-path qualifier, the resolved source names the
	// defining file (usable as a change block file-path), and resolution
	// reads an in-memory snapshot that does not reflect change blocks
	// applied during the session. See TheoryOfGoSrcBlocks.
	for name, prompt := range map[string]string{
		"GoSrcBlockSystemPrompt":  GoSrcBlockSystemPrompt,
		"GoSrcBlockRestatePrompt": GoSrcBlockRestatePrompt,
	} {
		if !strings.Contains(prompt, "full import path") {
			t.Fatalf("%s does not recommend the import-path qualifier", name)
		}
		if !strings.Contains(prompt, "file-path") {
			t.Fatalf("%s does not describe the defining file usage", name)
		}
		if !strings.Contains(prompt, "does not re-read") {
			t.Fatalf("%s does not describe the snapshot semantics", name)
		}
	}
}

func TestGoSrcPromptsEndWithSummary(t *testing.T) {
	// The go-src stop rule must be phrased like the shell prompt's: stop
	// generating, end the response with a summary block, and wait. A bare
	// "stop generating and wait" licenses omitting the summary block and
	// contradicts the every-response requirement of
	// blocks.SummaryBlockSystemPrompt. The prompt must also state the
	// sequence rule — the block after the go-src block's closing line
	// must be the summary block — so a response never ends on the go-src
	// block itself. See TheoryOfGoSrcBlocks.
	if !strings.Contains(GoSrcBlockSystemPrompt, "end the response with a summary block") {
		t.Fatal("system prompt must phrase the stop rule as ending the response with a summary block")
	}
	if strings.Contains(GoSrcBlockSystemPrompt, "stop generating and wait:") {
		t.Fatal("system prompt must not carry a stop instruction that omits the summary block")
	}
	if !strings.Contains(GoSrcBlockSystemPrompt, "Never end a response on a go-src block") {
		t.Fatal("system prompt must state the sequence rule: the block after a go-src block must be the summary block")
	}
}
