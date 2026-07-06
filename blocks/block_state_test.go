package blocks

import (
	"iter"
	"testing"

	"github.com/reusee/tai/generators"
)

// mockState is a minimal State implementation for testing BlockState.
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

func TestBlockStateStreamParsing(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// Fragment 1: prose only, no block marker
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("I'll fix the issue.\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks before any block marker, got %d", len(blocks))
	}

	// Fragment 2: opening marker and partial body (no end marker yet)
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks for incomplete block, got %d", len(blocks))
	}

	// Fragment 3: end marker completes the block
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::end 徕珑\n")},
	}); err != nil {
		t.Fatal(err)
	}
	blocks := state.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" {
		t.Fatalf("expected kind change, got %s", blocks[0].Kind)
	}
	if blocks[0].Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %s", blocks[0].Boundary)
	}

	// PopBlocks should have cleared the buffer
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks after pop, got %d", len(blocks))
	}
}

func TestBlockStateMultipleBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	text := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑\n:::change 栢彣\n<change op=\"DELETE\" target=\"Bar\" file-path=\"/test.go\" />\n:::end 栢彣\n:::finish 桀骥\nDone.\n:::end 桀骥\n"
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	blocks := state.PopBlocks()
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

func TestBlockStateUnwrapAndPassthrough(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	if state.Unwrap() != upstream {
		t.Fatal("Unwrap should return the upstream state")
	}
	if state.SystemPrompt() != "system prompt" {
		t.Fatalf("SystemPrompt should be %q, got %q", "system prompt", state.SystemPrompt())
	}
}

func TestBlockStateIgnoresUserRole(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// User role content should not be parsed for blocks
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text(":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑\n")},
	}); err != nil {
		t.Fatal(err)
	}

	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("user role content should not produce blocks, got %d", len(blocks))
	}
}

func TestBlockStateIgnoresThoughts(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// A Thought part containing complete block markers must not produce
	// a block, because thoughts are model reasoning, not block output.
	content := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{
			generators.Thought(":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑\n"),
		},
	}
	if _, err := state.AppendContent(content); err != nil {
		t.Fatal(err)
	}
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks from thought part, got %d", len(blocks))
	}
	if pending := state.PendingText(); pending != "" {
		t.Fatalf("expected empty buffer, got %q", pending)
	}

	// A Text part following a Thought part must still be parsed normally,
	// and the Thought's block markers must not combine with the Text.
	content2 := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{
			generators.Thought(":::change 栢彣\nbody\n:::end 栢彣\n"),
			generators.Text(":::change 瑱魃\n<change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\" />\n\nfunc Bar() {}\n:::end 瑱魃\n"),
		},
	}
	if _, err := state.AppendContent(content2); err != nil {
		t.Fatal(err)
	}
	blocks := state.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block from text part, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" || blocks[0].Boundary != "瑱魃" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", blocks[0].Kind, blocks[0].Boundary)
	}
}

func TestBlockStatePendingText(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// Append incomplete block
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("prose before\n:::change 徕珑\nbody")},
	}); err != nil {
		t.Fatal(err)
	}

	pending := state.PendingText()
	if pending == "" {
		t.Fatal("PendingText should not be empty for incomplete block")
	}
	if !contains(pending, ":::change 徕珑") {
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

func TestBlockStateNonMatchingEndIsBodyContent(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// The model opens a block with boundary 徕珑. The body contains a
	// line-start :::end with a different boundary (栢彣). This should be
	// treated as body content, not a closing marker. Since no matching
	// :::end 徕珑 exists, the block is unclosed (incomplete) and no
	// error should be surfaced during streaming.
	content := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(
			":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 栢彣\n",
		)},
	}
	_, err := state.AppendContent(content)
	if err != nil {
		t.Fatalf("expected no error for non-matching end marker treated as body content, got %v", err)
	}
	// No blocks should be produced for the incomplete block.
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks for unclosed block, got %d", len(blocks))
	}
	// The content should remain in the buffer as pending text.
	pending := state.PendingText()
	if !contains(pending, ":::change 徕珑") {
		t.Fatalf("pending text should contain the opening marker: %q", pending)
	}
}

func TestBlockStateNonMatchingEndInBodyThenMatchingEnd(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// A body containing a line-start :::end with a different boundary
	// is treated as body content. When the matching :::end 徕珑
	// arrives, the block is parsed correctly with the non-matching
	// :::end 栢彣 preserved in the body.
	text := ":::change 徕珑\nbody line 1\n:::end 栢彣\nbody line 2\n:::end 徕珑\n"
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	blocks := state.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" || blocks[0].Boundary != "徕珑" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", blocks[0].Kind, blocks[0].Boundary)
	}
	if !contains(blocks[0].Body, ":::end 栢彣") {
		t.Fatalf("body should contain non-matching :::end as content: %q", blocks[0].Body)
	}
	if !contains(blocks[0].Body, "body line 1") || !contains(blocks[0].Body, "body line 2") {
		t.Fatalf("body should contain both body lines: %q", blocks[0].Body)
	}
}

func TestBlockStateFlushTreatsUnclosedAsEnded(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// Append an unclosed block (no end marker yet).
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n")},
	}); err != nil {
		t.Fatal(err)
	}
	// No complete block before Flush.
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks before flush, got %d", len(blocks))
	}

	// Flush finalizes the unclosed block as ended.
	if _, err := state.Flush(); err != nil {
		t.Fatal(err)
	}
	blocks := state.PopBlocks()
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
	if pending := state.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text after flush, got %q", pending)
	}

	// Post-flush content must not combine with pre-flush content.
	// The orphan :::end marker produces no block.
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::end 徕珑\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks for orphan end marker after flush, got %d", len(blocks))
	}
}

func TestBlockStateEndMarkerNoTrailingNewline(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// The end marker is at the very end without a trailing newline.
	// The block should be parsed correctly during streaming.
	text := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑"
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	blocks := state.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" || blocks[0].Boundary != "徕珑" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", blocks[0].Kind, blocks[0].Boundary)
	}
	if !contains(blocks[0].Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", blocks[0].Body)
	}
	if contains(blocks[0].Body, ":::end") {
		t.Fatalf("body should not contain the end marker: %q", blocks[0].Body)
	}

	// No pending text should remain after a fully parsed block.
	if pending := state.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text, got %q", pending)
	}
}