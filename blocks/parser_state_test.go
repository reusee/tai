package blocks

import (
	"iter"
	"testing"

	"github.com/reusee/tai/generators"
)

// mockState is a minimal State implementation for testing ParserState.
type mockState struct {
	systemPrompt string
	contents     []*generators.Content
}

func (m *mockState) Contents() iter.Seq[*generators.Content] {
	return func(yield func(*generators.Content) bool) {
		for _, c := range m.contents {
			if !yield(c) {
				return
			}
		}
	}
}

func (m *mockState) AppendContent(content *generators.Content) (generators.State, error) {
	m.contents = append(m.contents, content)
	return m, nil
}

func (m *mockState) SystemPrompt() string {
	return m.systemPrompt
}

func (m *mockState) Functions() iter.Seq[*generators.Function] {
	return func(yield func(*generators.Function) bool) {}
}

func (m *mockState) Flush() (generators.State, error) {
	return m, nil
}

func (m *mockState) Unwrap() generators.State {
	return nil
}

func TestParserStateStreamParsing(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// Fragment 1: prose only, no block marker
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("I'll fix the issue.\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks before any block marker, got %d", len(collectedBlocks))
	}

	// Fragment 2: opening marker and partial body (no end marker yet)
	newState, err = ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks for incomplete block, got %d", len(collectedBlocks))
	}

	// Fragment 3: end marker completes the block
	newState, err = ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("徕珑龘\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" {
		t.Fatalf("expected kind change, got %s", collectedBlocks[0].Kind)
	}
	if collectedBlocks[0].Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", collectedBlocks[0].Boundary)
	}
}

func TestParserStateMultipleBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n<<龘靐齉 <change op=\"DELETE\" target=\"Bar\" file-path=\"/test.go\">\n龘靐齉\n<<齉爩龖 <summary>\n- Done.\n齉爩龖\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" || collectedBlocks[0].Boundary != "徕珑龘" {
		t.Fatalf("unexpected first block: %+v", collectedBlocks[0])
	}
	if collectedBlocks[1].Kind != "change" || collectedBlocks[1].Boundary != "龘靐齉" {
		t.Fatalf("unexpected second block: %+v", collectedBlocks[1])
	}
	if collectedBlocks[2].Kind != "summary" || collectedBlocks[2].Boundary != "齉爩龖" {
		t.Fatalf("unexpected third block: %+v", collectedBlocks[2])
	}
}

func TestParserStateUnwrapAndPassthrough(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewParserState(upstream)

	if state.Unwrap() != upstream {
		t.Fatal("Unwrap should return the upstream state")
	}
	if state.SystemPrompt() != "system prompt" {
		t.Fatalf("SystemPrompt should be %q, got %q", "system prompt", state.SystemPrompt())
	}
}

func TestParserStateIgnoresUserRole(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// User role content should not be parsed for blocks
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 0 {
		t.Fatalf("user role content should not produce blocks, got %d", len(collectedBlocks))
	}
}

func TestParserStateIgnoresThoughts(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// A Thought part containing complete block markers must not produce
	// a block, because thoughts are model reasoning, not block output.
	content := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{
			generators.Thought("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n"),
		},
	}
	newState, err := ps.AppendContent(content)
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks from thought part, got %d", len(collectedBlocks))
	}
	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty buffer, got %q", pending)
	}

	// A Text part following a Thought part must still be parsed normally,
	// and the Thought's block markers must not combine with the Text.
	content2 := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{
			generators.Thought("<<龘靐齉 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody\n龘靐齉\n"),
			generators.Text("<<齉爩龖 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\">\nfunc Bar() {}\n齉爩龖\n"),
		},
	}
	newState, err = ps.AppendContent(content2)
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block from text part, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" || collectedBlocks[0].Boundary != "齉爩龖" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", collectedBlocks[0].Kind, collectedBlocks[0].Boundary)
	}
}

func TestParserStatePendingText(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// Append incomplete block
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("prose before\n<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	pending := ps.PendingText()
	if pending == "" {
		t.Fatal("PendingText should not be empty for incomplete block")
	}
	if !contains(pending, "<<徕珑龘") {
		t.Fatalf("PendingText should contain the opening marker: %q", pending)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestParserStateNonMatchingEndIsBodyContent(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// The model opens a block with delimiter 徕珑龘. The body contains a
	// line with a different delimiter 龘靐齉. This should be
	// treated as body content, not a closing marker. Since no matching
	// 徕珑龘 line exists, the block is unclosed (incomplete) and no
	// error should be surfaced during streaming.
	content := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(
			"<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n龘靐齉\n",
		)},
	}
	newState, err := ps.AppendContent(content)
	if err != nil {
		t.Fatalf("expected no error for non-matching end marker treated as body content, got %v", err)
	}
	ps = newState.(*ParserState)
	// No blocks should be produced for the incomplete block.
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks for unclosed block, got %d", len(collectedBlocks))
	}
	// The content should remain in the buffer as pending text.
	pending := ps.PendingText()
	if !contains(pending, "<<徕珑龘") {
		t.Fatalf("pending text should contain the opening marker: %q", pending)
	}
}

func TestParserStateNonMatchingEndInBodyThenMatchingEnd(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// A body containing a line with a different delimiter 龘靐齉
	// is treated as body content. When the matching 徕珑龘
	// arrives, the block is parsed correctly with the non-matching
	// 龘靐齉 preserved in the body.
	text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody line 1\n龘靐齉\nbody line 2\n徕珑龘\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" || collectedBlocks[0].Boundary != "徕珑龘" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", collectedBlocks[0].Kind, collectedBlocks[0].Boundary)
	}
	if !contains(collectedBlocks[0].Body, "龘靐齉") {
		t.Fatalf("body should contain non-matching delimiter line as content: %q", collectedBlocks[0].Body)
	}
	if !contains(collectedBlocks[0].Body, "body line 1") || !contains(collectedBlocks[0].Body, "body line 2") {
		t.Fatalf("body should contain both body lines: %q", collectedBlocks[0].Body)
	}
}

func TestParserStateNestedBlocksSameDelimiter(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// The outer block contains a nested block with the same delimiter.
	// The nested block's closing marker must not prematurely close
	// the outer block. See TheoryOfNestedBlockParsing.
	text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<徕珑龘 <change op=\"MODIFY\" target=\"Inner\" file-path=\"/inner.go\">\nfunc Inner() {}\n徕珑龘\nfunc Outer() {}\n徕珑龘\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block (outer), got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", collectedBlocks[0].Boundary)
	}
	if !contains(collectedBlocks[0].Body, "func Inner() {}") {
		t.Fatalf("outer body should contain inner block body: %q", collectedBlocks[0].Body)
	}
	if !contains(collectedBlocks[0].Body, "func Outer() {}") {
		t.Fatalf("outer body should contain outer block body: %q", collectedBlocks[0].Body)
	}
	if !contains(collectedBlocks[0].Body, "<<徕珑龘") {
		t.Fatalf("outer body should contain inner block opening marker: %q", collectedBlocks[0].Body)
	}
}

func TestParserStateNestedDifferentDelimiterOpeningWithoutClosing(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// A body line that starts with "<<" and contains a valid XML tag
	// but uses a DIFFERENT delimiter from the outer block must be
	// treated as body content, not a nested opening. The outer block
	// should close at its own delimiter without being affected by the
	// different-delimiter opening. See TheoryOfNestedBlockParsing.
	text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\n<<龘靐齉 <tag>\nfunc Foo() {}\n徕珑龘\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", collectedBlocks[0].Boundary)
	}
	if !contains(collectedBlocks[0].Body, "<<龘靐齉 <tag>") {
		t.Fatalf("body should contain the different-delimiter opening as content: %q", collectedBlocks[0].Body)
	}
	if !contains(collectedBlocks[0].Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", collectedBlocks[0].Body)
	}
}

func TestParserStateFlushCollectsParseErrors(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// Append an unclosed block (no end marker yet).
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	// No complete block before Flush.
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks before flush, got %d", len(collectedBlocks))
	}

	// Flush must not return an error for the unclosed block. The block
	// is collected as a parse error instead, so the generation flow
	// continues and the caller can feed the error back to the model for
	// self-correction. See TheoryOfParseErrorCollection.
	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	parseErrors := ps.ParseErrors()
	if len(parseErrors) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(parseErrors))
	}
	if parseErrors[0].BlockKind != "change" || parseErrors[0].Boundary != "徕珑龘" {
		t.Fatalf("expected parse error kind=change boundary=徕珑龘, got kind=%q boundary=%q", parseErrors[0].BlockKind, parseErrors[0].Boundary)
	}
}

func TestParserStateFlushCollectsTruncatedOpeningLineParseError(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// The model output ends with an opening marker line that has no
	// trailing newline. This is a truncated block: the closing marker
	// must be alone on its own line, which cannot exist after EOF.
	// Flush must collect the truncation as a parse error instead of
	// silently dropping the block or aborting the flow.
	// See TheoryOfBlockFormat and TheoryOfParseErrorCollection.
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks before flush, got %d", len(collectedBlocks))
	}

	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush must not return an error for truncated opening line, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	parseErrors := ps.ParseErrors()
	if len(parseErrors) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(parseErrors))
	}
	if parseErrors[0].BlockKind != "change" || parseErrors[0].Boundary != "徕珑龘" {
		t.Fatalf("expected parse error kind=change boundary=徕珑龘, got kind=%q boundary=%q", parseErrors[0].BlockKind, parseErrors[0].Boundary)
	}
}

func TestParserStateFlushSucceedsWithCompleteBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// Append a complete block (with end marker).
	text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	// The complete block should already be parsed during AppendContent.
	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block before flush, got %d", len(collectedBlocks))
	}

	// Flush should succeed because there are no unclosed blocks.
	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush should succeed with no unclosed blocks, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	// No pending text should remain after flush.
	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text after flush, got %q", pending)
	}
}

func TestParserStateFlushCollectsMultipleParseErrors(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	// Two unclosed blocks separated by content. Both are collected as
	// parse errors during Flush; the scanner skips past each opening
	// marker to find the next block marker. See
	// TheoryOfParseErrorCollection.
	text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n" +
		"<<龘靐齉 <change op=\"DELETE\" target=\"Bar\" file-path=\"/test.go\">\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	parseErrors := ps.ParseErrors()
	if len(parseErrors) != 2 {
		t.Fatalf("expected 2 parse errors, got %d", len(parseErrors))
	}
	if parseErrors[0].Boundary != "徕珑龘" {
		t.Fatalf("expected first parse error boundary 徕珑龘, got %s", parseErrors[0].Boundary)
	}
	if parseErrors[1].Boundary != "龘靐齉" {
		t.Fatalf("expected second parse error boundary 龘靐齉, got %s", parseErrors[1].Boundary)
	}
}

func TestParserStateFlushCollectsParseErrorsAfterCompleteBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// A complete block followed by an unclosed block. The complete block
	// is parsed and handled during AppendContent; the unclosed block is
	// collected as a parse error during Flush. See
	// TheoryOfParseErrorCollection.
	text := "<<徕珑龘 <summary>\nDone.\n徕珑龘\n" +
		"<<龘靐齉 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 complete block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "summary" {
		t.Fatalf("expected summary block, got %s", collectedBlocks[0].Kind)
	}

	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	parseErrors := ps.ParseErrors()
	if len(parseErrors) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(parseErrors))
	}
	if parseErrors[0].BlockKind != "change" || parseErrors[0].Boundary != "龘靐齉" {
		t.Fatalf("expected parse error kind=change boundary=龘靐齉, got kind=%q boundary=%q", parseErrors[0].BlockKind, parseErrors[0].Boundary)
	}
}

func TestParserStateEndMarkerNoTrailingNewline(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// The end marker is at the very end without a trailing newline.
	// The block should be parsed correctly during streaming.
	text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" || collectedBlocks[0].Boundary != "徕珑龘" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", collectedBlocks[0].Kind, collectedBlocks[0].Boundary)
	}
	if !contains(collectedBlocks[0].Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", collectedBlocks[0].Body)
	}
	if contains(collectedBlocks[0].Body, "徕珑龘") {
		t.Fatalf("body should not contain the end marker: %q", collectedBlocks[0].Body)
	}

	// No pending text should remain after a fully parsed block.
	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text, got %q", pending)
	}
}

type testHandlerError struct {
	msg string
}

func TestParserStateBlockHandler(t *testing.T) {
	t.Run("ReceivesBlocks", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		var handledBlocks []Block
		ps := NewParserState(upstream, func(block Block) error {
			handledBlocks = append(handledBlocks, block)
			return nil
		})

		text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)

		if len(handledBlocks) != 1 {
			t.Fatalf("expected 1 handled block, got %d", len(handledBlocks))
		}
		if handledBlocks[0].Kind != "change" {
			t.Fatalf("expected change block, got %s", handledBlocks[0].Kind)
		}
	})

	t.Run("ErrorStopsStreaming", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		expectedErr := &testHandlerError{msg: "apply failed"}
		ps := NewParserState(upstream, func(block Block) error {
			if block.Kind == "change" {
				return expectedErr
			}
			return nil
		})

		text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n"
		_, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err == nil {
			t.Fatal("expected error from handler")
		}
		if err != expectedErr {
			t.Fatalf("expected %v, got %v", expectedErr, err)
		}
	})

	t.Run("HandlerPropagatedToNewState", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		var callCount int
		ps := NewParserState(upstream, func(block Block) error {
			callCount++
			return nil
		})

		text1 := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text1)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)
		if callCount != 1 {
			t.Fatalf("expected 1 handler call, got %d", callCount)
		}

		text2 := "<<龘靐齉 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\">\nfunc Bar() {}\n龘靐齉\n"
		newState, err = ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text2)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)
		if callCount != 2 {
			t.Fatalf("expected 2 handler calls, got %d", callCount)
		}
	})

	t.Run("HandlerNotCalledForUnclosedDuringFlush", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		var handledBlocks []Block
		ps := NewParserState(upstream, func(block Block) error {
			handledBlocks = append(handledBlocks, block)
			return nil
		})

		text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)

		if len(handledBlocks) != 0 {
			t.Fatalf("expected 0 handled blocks before flush, got %d", len(handledBlocks))
		}

		// Flush collects the unclosed block as a parse error instead of
		// returning an error. The handler is not called because the block
		// is incomplete. See TheoryOfParseErrorCollection.
		flushedState, err := ps.Flush()
		if err != nil {
			t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
		}
		ps = flushedState.(*ParserState)

		if len(handledBlocks) != 0 {
			t.Fatalf("expected 0 handled blocks after flush (unclosed block not applied), got %d", len(handledBlocks))
		}
		if len(ps.ParseErrors()) != 1 {
			t.Fatalf("expected 1 parse error, got %d", len(ps.ParseErrors()))
		}
	})

	t.Run("UnclosedBlockCollectedAsParseError", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		ps := NewParserState(upstream, func(block Block) error {
			return nil
		})

		// Unclosed change block (no closing marker) — simulates
		// truncated output where the model is cut off mid-block.
		text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {\n\t// truncated"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)

		// Flush must not return an error; the unclosed block is collected
		// as a parse error so the caller can feed it back to the model for
		// self-correction. See TheoryOfParseErrorCollection.
		flushedState, err := ps.Flush()
		if err != nil {
			t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
		}
		ps = flushedState.(*ParserState)

		parseErrors := ps.ParseErrors()
		if len(parseErrors) != 1 {
			t.Fatalf("expected 1 parse error, got %d", len(parseErrors))
		}
		if parseErrors[0].BlockKind != "change" || parseErrors[0].Boundary != "徕珑龘" {
			t.Fatalf("expected parse error kind=change boundary=徕珑龘, got kind=%q boundary=%q", parseErrors[0].BlockKind, parseErrors[0].Boundary)
		}
	})
}

func TestParserStateNoHandler(t *testing.T) {
	// When no handler is set, blocks are parsed but discarded.
	// This is used by commands that don't need post-phase block
	// processing (e.g., next with -no-apply).
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	text := "<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	// No error, blocks are simply discarded.
	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text after block parsed without handler, got %q", pending)
	}
}

func (e *testHandlerError) Error() string {
	return e.msg
}
