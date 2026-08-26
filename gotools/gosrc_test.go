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
	// The go-src system prompt must teach the package form: a symbol that
	// is a loaded package's exact import path or package name returns the
	// package's go doc documentation, with command and unexported
	// documentation for focus packages. See TheoryOfGoSrcBlocks.
	if !strings.Contains(GoSrcBlockSystemPrompt, "go doc documentation") ||
		!strings.Contains(GoSrcBlockSystemPrompt, "package name") {
		t.Fatal("GoSrcBlockSystemPrompt does not describe package symbols")
	}
}

func TestGoSrcPromptsPreferGoSrcOverRead(t *testing.T) {
	// The go-src prompt must teach the division of labor with read:
	// Go source is fetched by symbol — gaining the defining file and the
	// references report — while read serves non-Go files, whole-file
	// views, glob discovery, and network resources. See
	// TheoryOfGoSrcBlocks.
	if !strings.Contains(GoSrcBlockSystemPrompt, "Prefer go-src over read") {
		t.Fatal("system prompt does not teach the go-src preference for Go source")
	}
	if !strings.Contains(GoSrcBlockSystemPrompt, "references report") {
		t.Fatal("system prompt does not cite the references report as the reason for the preference")
	}
	if !strings.Contains(GoSrcBlockSystemPrompt, "non-Go files") {
		t.Fatal("system prompt does not delineate the read block's remaining uses")
	}
}

func TestGoSrcPromptsDescribeSnapshotAndFilePath(t *testing.T) {
	// The go-src system prompt must teach three facts about resolution
	// results: prefer the import-path qualifier, the resolved source names
	// the defining file (usable as a change block file-path), and
	// resolution reads an in-memory snapshot that does not reflect change
	// blocks applied during the session. See TheoryOfGoSrcBlocks.
	if !strings.Contains(GoSrcBlockSystemPrompt, "full import path") {
		t.Fatal("system prompt does not recommend the import-path qualifier")
	}
	if !strings.Contains(GoSrcBlockSystemPrompt, "file-path") {
		t.Fatal("system prompt does not describe the defining file usage")
	}
	if !strings.Contains(GoSrcBlockSystemPrompt, "does not re-read") {
		t.Fatal("system prompt does not describe the snapshot semantics")
	}
}

func TestGoSrcPromptsEndWithSummary(t *testing.T) {
	// The go-src stop rule must be phrased summary-first: emit the
	// summary block IMMEDIATELY after the last go-src block's closing
	// line, then end the response and wait. A bare "stop generating"
	// instruction placed before the summary requirement makes the model
	// halt at the closing line — the observed failure shape of a lone
	// go-src block ending a response — and contradicts the
	// every-response requirement of blocks.SummaryBlockSystemPrompt.
	// The prompt must also state the sequence rule — the block after the
	// go-src block's closing line must be the summary block. See
	// TheoryOfGoSrcBlocks.
	if !strings.Contains(GoSrcBlockSystemPrompt, "emit the summary block IMMEDIATELY") {
		t.Fatal("system prompt must phrase the stop rule summary-first: emit the summary block immediately after the last go-src block")
	}
	if strings.Contains(GoSrcBlockSystemPrompt, "stop generating") {
		t.Fatal("system prompt must not carry a bare stop instruction before the summary requirement")
	}
	if !strings.Contains(GoSrcBlockSystemPrompt, "never stop at") {
		t.Fatal("system prompt must forbid stopping at a go-src block's closing line")
	}
	if !strings.Contains(GoSrcBlockSystemPrompt, "Never end a response on a go-src block") {
		t.Fatal("system prompt must state the sequence rule: the block after a go-src block must be the summary block")
	}
}
