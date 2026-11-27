package selfprompts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
)

// testRoot opens a fresh filesystem root and returns it with its directory,
// so a test can assert both through the root and on disk.
func testRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root, dir
}

// testBlock builds a self-prompt block, leaving a parameter out when its
// value is empty so missing-parameter cases stay expressible.
func testBlock(op, name, body string) blocks.Block {
	attributes := map[string]string{}
	if op != "" {
		attributes["op"] = op
	}
	if name != "" {
		attributes["name"] = name
	}
	return blocks.Block{Kind: Kind, Attributes: attributes, Body: body}
}

func TestParseOperation(t *testing.T) {
	cases := []struct {
		name    string
		block   blocks.Block
		wantErr bool
	}{
		{"add", testBlock(OpAdd, "testing", "Run the tests."), false},
		{"modify", testBlock(OpModify, "testing", "Run the tests."), false},
		{"delete ignores the body", testBlock(OpDelete, "testing", ""), false},
		{"missing op", testBlock("", "testing", "x"), true},
		{"unknown op", testBlock("set", "testing", "x"), true},
		{"missing name", testBlock(OpAdd, "", "x"), true},
		{"name with a space", testBlock(OpAdd, "bad name", "x"), true},
		{"empty text", testBlock(OpAdd, "testing", "  \n "), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseOperation(c.block)
			if c.wantErr != (err != nil) {
				t.Fatalf("want error %v, got %v", c.wantErr, err)
			}
		})
	}
}

func TestApplyAddModifyDelete(t *testing.T) {
	root, dir := testRoot(t)
	if _, err := Apply(root, []blocks.Block{
		testBlock(OpAdd, "tests", "Run the tests before claiming a fix."),
		testBlock(OpAdd, "tone", "Write short sentences."),
	}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, []blocks.Block{
		testBlock(OpModify, "tone", "Write plain sentences."),
		testBlock(OpDelete, "tests", ""),
	}, true); err != nil {
		t.Fatal(err)
	}
	entries, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %v", entries)
	}
	if entries["tone"] != "Write plain sentences." {
		t.Fatalf("got %q", entries["tone"])
	}
}

func TestApplyBatchIsAtomic(t *testing.T) {
	root, dir := testRoot(t)
	_, err := Apply(root, []blocks.Block{
		testBlock(OpAdd, "ok", "A sound directive."),
		testBlock(OpModify, "missing", "A directive."),
	}, true)
	var rejected *RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(dir + "/" + selfPromptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the rejected batch wrote the file: %v", err)
	}
}

func TestApplyRejectsEveryProblem(t *testing.T) {
	root, _ := testRoot(t)
	_, err := Apply(root, []blocks.Block{
		testBlock(OpModify, "missing", "A directive."),
		testBlock(OpDelete, "gone", ""),
		testBlock("bogus", "name", "x"),
	}, true)
	var rejected *RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("got %v", err)
	}
	if len(rejected.Problems) != 3 {
		t.Fatalf("got %v", rejected.Problems)
	}
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	root, dir := testRoot(t)
	ops, err := Apply(root, []blocks.Block{
		testBlock(OpAdd, "tone", "Write short sentences."),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("got %v", ops)
	}
	if _, err := os.Stat(dir + "/" + selfPromptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the dry run wrote the file: %v", err)
	}
}

func TestApplyRejectsOversizedMap(t *testing.T) {
	root, _ := testRoot(t)
	text := strings.Repeat("x", maxEntryRunes-10)
	count := maxTotalRunes/(maxEntryRunes-10) + 2
	bs := make([]blocks.Block, 0, count)
	for i := range count {
		bs = append(bs, testBlock(OpAdd, fmt.Sprintf("entry%d", i), text))
	}
	_, err := Apply(root, bs, true)
	var rejected *RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("got %v", err)
	}
}

func TestRenderEntries(t *testing.T) {
	content := Render(Entries{
		"tone":  "Write short sentences.",
		"tests": "Run the tests.\nThen report.",
	})
	if !strings.Contains(content, "- tests: Run the tests.\n  Then report.\n") {
		t.Fatalf("got %q", content)
	}
	if strings.Index(content, "- tests:") > strings.Index(content, "- tone:") {
		t.Fatalf("the names must render in sorted order: %q", content)
	}
	if content := Render(Entries{}); content != "" {
		t.Fatalf("an empty map must render nothing, got %q", content)
	}
}

func TestContentReadsWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	if content := Content(); content != "" {
		t.Fatalf("a missing file must render nothing, got %q", content)
	}
	if err := os.MkdirAll(selfPromptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selfPromptPath, []byte(`{"tone": "Write short sentences."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if content := Content(); !strings.Contains(content, "- tone: Write short sentences.") {
		t.Fatalf("got %q", content)
	}
}

func TestComponentSetPromptSections(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(selfPromptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selfPromptPath, []byte(`{"tone": "Write short sentences."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sections := components.ComponentSet{
		NewComponent(false),
		NewContentComponent(),
	}.PromptSections()
	if !strings.Contains(sections, "op=add") {
		t.Fatal("the kind prompt is missing")
	}
	if !strings.Contains(sections, "- tone: Write short sentences.") {
		t.Fatal("the entry is missing")
	}
	if strings.Index(sections, "op=add") > strings.Index(sections, "- tone:") {
		t.Fatal("the content must follow the kind prompt")
	}
	if sections := (components.ComponentSet{NewComponent(false)}).PromptSections(); strings.Contains(sections, "- tone:") {
		t.Fatal("the kind component must not carry the entries")
	}
}

func TestProcessBatchFeedback(t *testing.T) {
	root, _ := testRoot(t)
	rejectedResult := processBatch(&components.ProcessContext{
		Blocks: []blocks.Block{testBlock(OpModify, "missing", "A directive.")},
		Root:   root,
	}, true)
	if rejectedResult.Err != nil {
		t.Fatal(rejectedResult.Err)
	}
	if len(rejectedResult.Parts) != 1 {
		t.Fatalf("got %v", rejectedResult.Parts)
	}
	text, ok := rejectedResult.Parts[0].(generators.Text)
	if !ok {
		t.Fatalf("got %T", rejectedResult.Parts[0])
	}
	if !strings.Contains(string(text), "no such entry") {
		t.Fatalf("got %q", text)
	}

	appliedResult := processBatch(&components.ProcessContext{
		Blocks: []blocks.Block{testBlock(OpAdd, "tone", "Write short sentences.")},
		Root:   root,
	}, false)
	if appliedResult.Err != nil {
		t.Fatal(appliedResult.Err)
	}
	if len(appliedResult.Parts) != 1 {
		t.Fatalf("got %v", appliedResult.Parts)
	}
	text, ok = appliedResult.Parts[0].(generators.Text)
	if !ok {
		t.Fatalf("got %T", appliedResult.Parts[0])
	}
	if !strings.Contains(string(text), "dry run") {
		t.Fatalf("got %q", text)
	}
}

// TestComponentProcessWritesThroughRoot checks the component's own path from
// ProcessContext to the file: an applied batch writes .tai/self-prompt and
// reports it as round feedback, a dry run reports the same batch and leaves
// the tree alone. See TheoryOfSelfPrompt.
func TestComponentProcessWritesThroughRoot(t *testing.T) {
	root, dir := testRoot(t)
	path := dir + "/" + selfPromptPath

	comp := NewComponent(false)
	result := comp.Process(context.Background(), &components.ProcessContext{
		Blocks: []blocks.Block{testBlock(OpAdd, "tone", "Write short sentences.")},
		Root:   root,
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if len(result.Parts) != 1 {
		t.Fatalf("an applied batch must trigger the next round, got %v", result.Parts)
	}
	entries, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if entries["tone"] != "Write short sentences." {
		t.Fatalf("got %v", entries)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	result = NewComponent(true).Process(context.Background(), &components.ProcessContext{
		Blocks: []blocks.Block{testBlock(OpAdd, "tone", "Write short sentences.")},
		Root:   root,
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if len(result.Parts) != 1 {
		t.Fatalf("a dry run must still report the batch, got %v", result.Parts)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the dry run wrote the file: %v", err)
	}
}
