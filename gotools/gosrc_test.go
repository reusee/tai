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

// TestGoSrcPrompts verifies the fragments the go-src system prompt
// must teach: the package-symbol form, the division of labor with
// ingest, batch fetching, the resolution-result contract, and the
// summary-first stop rule. See TheoryOfGoSrcBlocks and
// TheoryOfGoSrcResolution.
func TestGoSrcPrompts(t *testing.T) {
	prompt := GoSrcBlockSystemPrompt

	t.Run("PackageSymbols", func(t *testing.T) {
		if !strings.Contains(prompt, "go doc documentation") ||
			!strings.Contains(prompt, "package name") {
			t.Fatal("GoSrcBlockSystemPrompt does not describe package symbols")
		}
	})

	t.Run("UnloadedPackagePath", func(t *testing.T) {
		if !strings.Contains(prompt, "even when the session never loaded the package") {
			t.Fatal("GoSrcBlockSystemPrompt must teach that an unloaded import path still resolves to its go doc documentation")
		}
	})

	t.Run("PreferOverIngest", func(t *testing.T) {
		if !strings.Contains(prompt, "Prefer go-src over ingest") {
			t.Fatal("GoSrcBlockSystemPrompt does not teach the go-src preference for Go source")
		}
		if !strings.Contains(prompt, "references report") {
			t.Fatal("GoSrcBlockSystemPrompt does not cite the references report as the reason for the preference")
		}
		if !strings.Contains(prompt, "non-Go files") {
			t.Fatal("GoSrcBlockSystemPrompt does not delineate the ingest block's remaining uses")
		}
		if !strings.Contains(prompt, "information about a specific package") {
			t.Fatal("GoSrcBlockSystemPrompt does not teach preferring go-src for package information")
		}
	})

	t.Run("BatchFetch", func(t *testing.T) {
		if !strings.Contains(prompt, "Batch fetches: collect every symbol you expect to need into one go-src block") {
			t.Fatal("go-src prompt must teach batching every needed symbol into one block to minimize fetching rounds")
		}
	})

	t.Run("ResultsAndFilePath", func(t *testing.T) {
		if !strings.Contains(prompt, "full import path") {
			t.Fatal("GoSrcBlockSystemPrompt does not recommend the import-path qualifier")
		}
		if !strings.Contains(prompt, "file-path") {
			t.Fatal("GoSrcBlockSystemPrompt does not describe the defining file usage")
		}
		if strings.Contains(prompt, "in-memory snapshot") {
			t.Fatal("GoSrcBlockSystemPrompt must not expose the snapshot semantics")
		}
		if strings.Contains(prompt, "Verify applied changes") {
			t.Fatal("GoSrcBlockSystemPrompt must not instruct disk verification")
		}
	})

	t.Run("SummaryStopRule", func(t *testing.T) {
		if !strings.Contains(prompt, "emit the summary block IMMEDIATELY") {
			t.Fatal("system prompt must phrase the stop rule summary-first: emit the summary block immediately after the last go-src block")
		}
		if strings.Contains(prompt, "stop generating") {
			t.Fatal("system prompt must not carry a bare stop instruction before the summary requirement")
		}
		if !strings.Contains(prompt, "never stop at") {
			t.Fatal("system prompt must forbid stopping at a go-src block's closing line")
		}
		if !strings.Contains(prompt, "Never end a response on a go-src block") {
			t.Fatal("system prompt must state the sequence rule: the block after a go-src block must be the summary block")
		}
	})
}
