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
	"github.com/reusee/tai/pathutil"
)

// testDir returns a fresh tai dir: the directory whose .tai the test writes.
func testDir(t *testing.T) pathutil.TaiDir {
	t.Helper()
	return pathutil.TaiDir(t.TempDir())
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
	dir := testDir(t)
	if _, err := Apply(dir, []blocks.Block{
		testBlock(OpAdd, "tests", "Run the tests before claiming a fix."),
		testBlock(OpAdd, "tone", "Write short sentences."),
	}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(dir, []blocks.Block{
		testBlock(OpModify, "tone", "Write plain sentences."),
		testBlock(OpDelete, "tests", ""),
	}, true); err != nil {
		t.Fatal(err)
	}
	entries, err := load(dir)
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
	dir := testDir(t)
	_, err := Apply(dir, []blocks.Block{
		testBlock(OpAdd, "ok", "A sound directive."),
		testBlock(OpModify, "missing", "A directive."),
	}, true)
	var rejected *RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(selfPromptFilePath(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the rejected batch wrote the file: %v", err)
	}
}

func TestApplyRejectsEveryProblem(t *testing.T) {
	dir := testDir(t)
	_, err := Apply(dir, []blocks.Block{
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
	dir := testDir(t)
	ops, err := Apply(dir, []blocks.Block{
		testBlock(OpAdd, "tone", "Write short sentences."),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("got %v", ops)
	}
	if _, err := os.Stat(selfPromptFilePath(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the dry run wrote the file: %v", err)
	}
}

func TestApplyRejectsOversizedMap(t *testing.T) {
	dir := testDir(t)
	text := strings.Repeat("x", maxEntryRunes-10)
	count := maxTotalRunes/(maxEntryRunes-10) + 2
	bs := make([]blocks.Block, 0, count)
	for i := range count {
		bs = append(bs, testBlock(OpAdd, fmt.Sprintf("entry%d", i), text))
	}
	_, err := Apply(dir, bs, true)
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

// TestContentReadsTaiDir checks that the entries are read from the tai dir
// the caller passes: the component renders the state of that directory and
// no other. The resolution of the tai dir itself is
// pathutil.TestModuleTaiDir's. See TheoryOfSelfPrompt.
func TestContentReadsTaiDir(t *testing.T) {
	dir := testDir(t)
	if content := Content(dir); content != "" {
		t.Fatalf("a missing file must render nothing, got %q", content)
	}
	if err := os.MkdirAll(dir.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selfPromptFilePath(dir), []byte(`{"tone": "Write short sentences."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if content := Content(dir); !strings.Contains(content, "- tone: Write short sentences.") {
		t.Fatalf("got %q", content)
	}
}

func TestComponentSetPromptSections(t *testing.T) {
	dir := testDir(t)
	if err := os.MkdirAll(dir.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selfPromptFilePath(dir), []byte(`{"tone": "Write short sentences."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sections := components.ComponentSet{
		NewComponent(dir, false),
		NewContentComponent(dir),
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
	if sections := (components.ComponentSet{NewComponent(dir, false)}).PromptSections(); strings.Contains(sections, "- tone:") {
		t.Fatal("the kind component must not carry the entries")
	}
}

func TestProcessBatchFeedback(t *testing.T) {
	dir := testDir(t)
	rejectedResult := processBatch(dir, &components.ProcessContext{
		Blocks: []blocks.Block{testBlock(OpModify, "missing", "A directive.")},
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

	appliedResult := processBatch(dir, &components.ProcessContext{
		Blocks: []blocks.Block{testBlock(OpAdd, "tone", "Write short sentences.")},
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

// TestComponentProcessWritesTaiDir checks the component's own path from a
// batch to the file: an applied batch writes the self-prompt file in the
// given tai dir and reports it as round feedback, a dry run reports the same
// batch and leaves the tree alone. See TheoryOfSelfPrompt.
func TestComponentProcessWritesTaiDir(t *testing.T) {
	dir := testDir(t)
	path := selfPromptFilePath(dir)

	comp := NewComponent(dir, false)
	result := comp.Process(context.Background(), &components.ProcessContext{
		Blocks: []blocks.Block{testBlock(OpAdd, "tone", "Write short sentences.")},
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if len(result.Parts) != 1 {
		t.Fatalf("an applied batch must trigger the next round, got %v", result.Parts)
	}
	entries, err := load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if entries["tone"] != "Write short sentences." {
		t.Fatalf("got %v", entries)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	result = NewComponent(dir, true).Process(context.Background(), &components.ProcessContext{
		Blocks: []blocks.Block{testBlock(OpAdd, "tone", "Write short sentences.")},
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
