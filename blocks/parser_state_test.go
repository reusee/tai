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
	ps := NewParserState(upstream)

	// Fragment 1: prose only, no block marker
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("I'll fix the issue.\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	blocks, ps := ps.PopBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks before any block marker, got %d", len(blocks))
	}

	// Fragment 2: opening marker and partial body (no end marker yet)
	newState, err = ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	blocks, ps = ps.PopBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks for incomplete block, got %d", len(blocks))
	}

	// Fragment 3: end marker completes the block
	newState, err = ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::徕珑 </change>\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	blocks, ps = ps.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" {
		t.Fatalf("expected kind change, got %s", blocks[0].Kind)
	}
	if blocks[0].Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %s", blocks[0].Boundary)
	}

	// PopBlocks should have cleared the blocks
	if remaining, _ := ps.PopBlocks(); len(remaining) != 0 {
		t.Fatalf("expected 0 blocks after pop, got %d", len(remaining))
	}
}

func TestParserStateMultipleBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	text := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n:::栢彣 <change op=\"DELETE\" target=\"Bar\" file-path=\"/test.go\">\n:::栢彣 </change>\n:::桀骥 <finish>\nDone.\n:::桀骥 </finish>\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	blocks, _ := ps.PopBlocks()
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" || blocks[0].Boundary != "徕珑" {
		t.Fatalf("unexpected first block: %+v", blocks[0])
	}
	if blocks[1].Kind != "change" || blocks[1].Boundary != "栢彣" {
		t.Fatalf("unexpected second block: %+v", blocks[1])
	}
	if blocks[2].Kind != "finish" || blocks[2].Boundary != "桀骥" {
		t.Fatalf("unexpected third block: %+v", blocks[2])
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
	ps := NewParserState(upstream)

	// User role content should not be parsed for blocks
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	blocks, _ := ps.PopBlocks()
	if len(blocks) != 0 {
		t.Fatalf("user role content should not produce blocks, got %d", len(blocks))
	}
}

func TestParserStateIgnoresThoughts(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	// A Thought part containing complete block markers must not produce
	// a block, because thoughts are model reasoning, not block output.
	content := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{
			generators.Thought(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n"),
		},
	}
	newState, err := ps.AppendContent(content)
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	blocks, ps := ps.PopBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks from thought part, got %d", len(blocks))
	}
	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty buffer, got %q", pending)
	}

	// A Text part following a Thought part must still be parsed normally,
	// and the Thought's block markers must not combine with the Text.
	content2 := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{
			generators.Thought(":::栢彣 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody\n:::栢彣 </change>\n"),
			generators.Text(":::瑱魃 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\">\nfunc Bar() {}\n:::瑱魃 </change>\n"),
		},
	}
	newState, err = ps.AppendContent(content2)
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	blocks, _ = ps.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block from text part, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" || blocks[0].Boundary != "瑱魃" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", blocks[0].Kind, blocks[0].Boundary)
	}
}

func TestParserStatePendingText(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	// Append incomplete block
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("prose before\n:::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	pending := ps.PendingText()
	if pending == "" {
		t.Fatal("PendingText should not be empty for incomplete block")
	}
	if !contains(pending, ":::徕珑") {
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
	ps := NewParserState(upstream)

	// The model opens a block with boundary 徕珑. The body contains a
	// line-start :::栢彣 </change> with a different boundary. This should be
	// treated as body content, not a closing marker. Since no matching
	// :::徕珑 </change> exists, the block is unclosed (incomplete) and no
	// error should be surfaced during streaming.
	content := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(
			":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::栢彣 </change>\n",
		)},
	}
	newState, err := ps.AppendContent(content)
	if err != nil {
		t.Fatalf("expected no error for non-matching end marker treated as body content, got %v", err)
	}
	ps = newState.(*ParserState)
	// No blocks should be produced for the incomplete block.
	blocks, ps := ps.PopBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks for unclosed block, got %d", len(blocks))
	}
	// The content should remain in the buffer as pending text.
	pending := ps.PendingText()
	if !contains(pending, ":::徕珑") {
		t.Fatalf("pending text should contain the opening marker: %q", pending)
	}
}

func TestParserStateNonMatchingEndInBodyThenMatchingEnd(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	// A body containing a line-start :::栢彣 </change> with a different boundary
	// is treated as body content. When the matching :::徕珑 </change>
	// arrives, the block is parsed correctly with the non-matching
	// :::栢彣 </change> preserved in the body.
	text := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody line 1\n:::栢彣 </change>\nbody line 2\n:::徕珑 </change>\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	blocks, _ := ps.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" || blocks[0].Boundary != "徕珑" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", blocks[0].Kind, blocks[0].Boundary)
	}
	if !contains(blocks[0].Body, ":::栢彣 </change>") {
		t.Fatalf("body should contain non-matching closing marker as content: %q", blocks[0].Body)
	}
	if !contains(blocks[0].Body, "body line 1") || !contains(blocks[0].Body, "body line 2") {
		t.Fatalf("body should contain both body lines: %q", blocks[0].Body)
	}
}

func TestParserStateFlushTreatsUnclosedAsEnded(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	// Append an unclosed block (no end marker yet).
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	// No complete block before Flush.
	blocks, ps := ps.PopBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks before flush, got %d", len(blocks))
	}

	// Flush finalizes the unclosed block as ended.
	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatal(err)
	}
	ps = flushedState.(*ParserState)
	blocks, ps = ps.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block after flush, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" || blocks[0].Boundary != "徕珑" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", blocks[0].Kind, blocks[0].Boundary)
	}
	if !contains(blocks[0].Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", blocks[0].Body)
	}
	// The buffer is fully consumed at Flush.
	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text after flush, got %q", pending)
	}

	// Post-flush content must not combine with pre-flush content.
	// The orphan closing marker produces no block.
	newState, err = ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::徕珑 </change>\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	blocks, _ = ps.PopBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks for orphan end marker after flush, got %d", len(blocks))
	}
}

func TestParserStateEndMarkerNoTrailingNewline(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	// The end marker is at the very end without a trailing newline.
	// The block should be parsed correctly during streaming.
	text := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	blocks, ps := ps.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" || blocks[0].Boundary != "徕珑" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", blocks[0].Kind, blocks[0].Boundary)
	}
	if !contains(blocks[0].Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", blocks[0].Body)
	}
	if contains(blocks[0].Body, ":::徕珑") {
		t.Fatalf("body should not contain the end marker: %q", blocks[0].Body)
	}

	// No pending text should remain after a fully parsed block.
	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text, got %q", pending)
	}
}

func TestParserStateHasCompletionBlock(t *testing.T) {
	t.Run("SummaryBlock", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		ps := NewParserState(upstream)
		text := ":::徕珑 <summary>\nDone.\n:::徕珑 </summary>\n"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)
		if !ps.HasCompletionBlock() {
			t.Fatal("expected HasCompletionBlock to return true for summary block")
		}
	})

	t.Run("FinishBlock", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		ps := NewParserState(upstream)
		text := ":::徕珑 <finish>\nDone.\n:::徕珑 </finish>\n"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)
		if !ps.HasCompletionBlock() {
			t.Fatal("expected HasCompletionBlock to return true for finish block")
		}
	})

	t.Run("ChangeBlockOnly", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		ps := NewParserState(upstream)
		text := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)
		if ps.HasCompletionBlock() {
			t.Fatal("expected HasCompletionBlock to return false for change block only")
		}
	})

	t.Run("NilState", func(t *testing.T) {
		var ps *ParserState
		if ps.HasCompletionBlock() {
			t.Fatal("expected HasCompletionBlock to return false for nil state")
		}
	})
}
